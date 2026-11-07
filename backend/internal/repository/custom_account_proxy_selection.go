package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// accountProxySelectionLockKey 事务级代理分配锁，least_used 与一对一未绑定选取共用，避免并发抢同一批代理。
const accountProxySelectionLockKey int64 = 742319830125

const unusedActiveProxyWhere = `p.deleted_at IS NULL AND p.status='active' AND (p.expires_at IS NULL OR p.expires_at>NOW())
  AND NOT EXISTS (
    SELECT 1 FROM accounts a
    WHERE a.deleted_at IS NULL AND a.proxy_id = p.id
  )`

// SelectAccountProxy 在数据库内选出单个 ID，不把整个代理池或密码载入应用。
// 最空闲选择持有事务级锁到绑定提交，防止并发推送全部观察到同一份空闲计数。
func (r *proxyRepository) SelectAccountProxy(ctx context.Context, mode string) (int64, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return 0, fmt.Errorf("代理选择需要写事务")
	}
	client := tx.Client()
	query := `SELECT p.id FROM proxies p WHERE p.deleted_at IS NULL AND p.status='active' AND (p.expires_at IS NULL OR p.expires_at>NOW()) ORDER BY random() LIMIT 1`
	switch mode {
	case "random":
	case "least_used":
		if _, err := client.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, accountProxySelectionLockKey); err != nil {
			return 0, err
		}
		query = `WITH bindings AS (
   SELECT a.id AS account_id,a.proxy_id FROM accounts a WHERE a.deleted_at IS NULL AND a.proxy_id IS NOT NULL
  ), counts AS (SELECT proxy_id,COUNT(*) AS n FROM bindings GROUP BY proxy_id)
  SELECT p.id FROM proxies p LEFT JOIN counts c ON c.proxy_id=p.id
  WHERE p.deleted_at IS NULL AND p.status='active' AND (p.expires_at IS NULL OR p.expires_at>NOW())
  ORDER BY COALESCE(c.n,0),p.id LIMIT 1`
	default:
		return 0, fmt.Errorf("不支持的代理选择模式")
	}
	rows, err := client.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var id int64
	if rows.Next() {
		if err = rows.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, rows.Err()
}

// CountUnusedActiveProxies 返回当前未被任何未删除账号绑定的活跃、未过期代理数量（只读，不加锁）。
func (r *proxyRepository) CountUnusedActiveProxies(ctx context.Context) (int64, error) {
	client := r.client
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	rows, err := client.QueryContext(ctx, `SELECT COUNT(*) FROM proxies p WHERE `+unusedActiveProxyWhere)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var n int64
	if rows.Next() {
		if err = rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}

// SelectUnusedActiveProxies 在写事务中锁定并选出最多 n 个未绑定活跃代理 ID（按 id 升序）。
// 调用方须在同一事务内完成账号绑定提交，否则并发批次可能观察到同一批空闲代理。
func (r *proxyRepository) SelectUnusedActiveProxies(ctx context.Context, n int) ([]int64, error) {
	if n <= 0 {
		return nil, nil
	}
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, fmt.Errorf("代理选择需要写事务")
	}
	client := tx.Client()
	if _, err := client.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, accountProxySelectionLockKey); err != nil {
		return nil, err
	}
	rows, err := client.QueryContext(ctx, `
SELECT p.id FROM proxies p
WHERE `+unusedActiveProxyWhere+`
ORDER BY p.id
LIMIT $1`, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, n)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
