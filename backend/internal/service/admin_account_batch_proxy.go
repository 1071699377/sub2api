package service

import (
	"context"
	"fmt"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const ProxyAssignOneToOne = "one_to_one"

// unusedProxySelector is implemented by the proxy repository for exclusive one-to-one assignment.
type unusedProxySelector interface {
	SelectUnusedActiveProxies(ctx context.Context, n int) ([]int64, error)
	CountUnusedActiveProxies(ctx context.Context) (int64, error)
}

// ErrProxyPoolInsufficient builds PROXY_POOL_INSUFFICIENT with needed/available metadata.
func ErrProxyPoolInsufficient(needed, available int) error {
	return infraerrors.BadRequest(
		"PROXY_POOL_INSUFFICIENT",
		fmt.Sprintf("空闲代理不足：需要 %d，可用 %d", needed, available),
	).WithMetadata(map[string]string{
		"needed":    strconv.Itoa(needed),
		"available": strconv.Itoa(available),
	})
}

// CountUnusedActiveProxies returns unused active non-expired proxies for UI preflight.
func (s *adminServiceImpl) CountUnusedActiveProxies(ctx context.Context) (int64, error) {
	repo, ok := s.proxyRepo.(unusedProxySelector)
	if !ok {
		return 0, fmt.Errorf("未绑定代理统计服务不可用")
	}
	return repo.CountUnusedActiveProxies(ctx)
}

// BatchCreateAccountsOneToOne assigns distinct unused proxies to each input and creates all
// accounts in one write transaction. Insufficient proxies rejects the entire batch.
func (s *adminServiceImpl) BatchCreateAccountsOneToOne(ctx context.Context, inputs []*CreateAccountInput) ([]*Account, error) {
	if len(inputs) == 0 {
		return nil, infraerrors.BadRequest("INVALID_REQUEST", "accounts is required")
	}
	for _, input := range inputs {
		if input == nil {
			return nil, infraerrors.BadRequest("INVALID_REQUEST", "account entry cannot be null")
		}
		if input.ProxyID != nil && *input.ProxyID != 0 {
			return nil, infraerrors.BadRequest("PROXY_SELECTION_CONFLICT", "一对一自动分配不能同时指定 proxy_id")
		}
	}
	if s.entClient == nil {
		return nil, fmt.Errorf("一对一代理批量创建需要数据库事务")
	}
	repo, ok := s.proxyRepo.(unusedProxySelector)
	if !ok {
		return nil, fmt.Errorf("一对一代理选择服务不可用")
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txctx := dbent.NewTxContext(ctx, tx)

	needed := len(inputs)
	ids, err := repo.SelectUnusedActiveProxies(txctx, needed)
	if err != nil {
		return nil, err
	}
	if len(ids) < needed {
		return nil, ErrProxyPoolInsufficient(needed, len(ids))
	}

	created := make([]*Account, 0, needed)
	for i, input := range inputs {
		safe := *input
		pid := ids[i]
		safe.ProxyID = &pid
		account, err := s.CreateAccount(txctx, &safe)
		if err != nil {
			return nil, err
		}
		created = append(created, account)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}
