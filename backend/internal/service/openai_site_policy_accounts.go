package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

const (
	openAISitePolicyAccountPageSize   = 200
	openAISitePolicyMaxUpstreamProbes = 20
)

// OpenAISitePolicyApplyResult 是「应用白名单到现有账号」的结果统计。
type OpenAISitePolicyApplyResult struct {
	Updated int      `json:"updated"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

// OpenAISiteUpstreamModelsResult 是「从上游同步候选模型」的结果。
type OpenAISiteUpstreamModelsResult struct {
	Models         []string `json:"models"`
	AccountsProbed int      `json:"accounts_probed"`
	AccountsFailed int      `json:"accounts_failed"`
	Errors         []string `json:"errors,omitempty"`
}

// listOpenAIAccountsForSitePolicy 分页列出全部 OpenAI 账号（不含 spark 影子账号）。
func listOpenAIAccountsForSitePolicy(ctx context.Context, admin AdminService) ([]Account, error) {
	if admin == nil {
		return nil, errors.New("admin service is not configured")
	}
	var out []Account
	for page := 1; ; page++ {
		accounts, total, err := admin.ListAccounts(ctx, page, openAISitePolicyAccountPageSize, PlatformOpenAI, "", "", "", 0, "", AccountUsageRangeFilter{}, "id", "asc")
		if err != nil {
			return nil, err
		}
		for _, account := range accounts {
			if account.Platform != PlatformOpenAI || account.IsCredentialShadow() {
				continue
			}
			out = append(out, account)
		}
		if len(accounts) == 0 || int64(page*openAISitePolicyAccountPageSize) >= total {
			break
		}
	}
	return out, nil
}

// ApplyOpenAISiteModelsToAccounts 把当前站点白名单写入所有 OpenAI 账号的 model_mapping。
// 白名单为空（不限制）时清空各账号的 model_mapping，账号恢复为支持全部模型。
func ApplyOpenAISiteModelsToAccounts(ctx context.Context, admin AdminService) (*OpenAISitePolicyApplyResult, error) {
	accounts, err := listOpenAIAccountsForSitePolicy(ctx, admin)
	if err != nil {
		return nil, err
	}
	mapping := openAISiteModelMapping()
	result := &OpenAISitePolicyApplyResult{}
	for i := range accounts {
		account := &accounts[i]
		// 复制完整 credentials：UpdateAccount 的合并语义下，未带上的非敏感键会被删除。
		credentials := make(map[string]any, len(account.Credentials)+1)
		for k, v := range account.Credentials {
			credentials[k] = v
		}
		if mapping != nil {
			credentials["model_mapping"] = mapping
		} else {
			if _, ok := credentials["model_mapping"]; !ok {
				result.Skipped++
				continue
			}
			delete(credentials, "model_mapping")
		}
		if len(credentials) == 0 {
			// UpdateAccount 只在 credentials 非空时落库；这里补一个空映射占位。
			credentials["model_mapping"] = map[string]any{}
		}
		if _, err := admin.UpdateAccount(ctx, account.ID, &UpdateAccountInput{Credentials: credentials}); err != nil {
			result.Failed++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, fmt.Sprintf("account %d: %v", account.ID, err))
			}
			slog.Warn("openai_site_policy_apply_account_failed", "account_id", account.ID, "error", err)
			continue
		}
		result.Updated++
	}
	return result, nil
}

// upstreamModelFetcher 抽象 AccountTestService.FetchUpstreamSupportedModels，便于测试。
type upstreamModelFetcher interface {
	FetchUpstreamSupportedModels(ctx context.Context, account *Account) ([]string, error)
}

// CollectOpenAISiteUpstreamModels 依次从可用的 OpenAI 账号拉取上游模型列表并取并集，
// 作为站点白名单的候选。最多探测 openAISitePolicyMaxUpstreamProbes 个账号。
func CollectOpenAISiteUpstreamModels(ctx context.Context, admin AdminService, fetcher upstreamModelFetcher) (*OpenAISiteUpstreamModelsResult, error) {
	if fetcher == nil {
		return nil, errors.New("account test service is not configured")
	}
	accounts, err := listOpenAIAccountsForSitePolicy(ctx, admin)
	if err != nil {
		return nil, err
	}
	result := &OpenAISiteUpstreamModelsResult{Models: []string{}}
	var collected []string
	for i := range accounts {
		account := &accounts[i]
		if !account.IsActive() {
			continue
		}
		if result.AccountsProbed >= openAISitePolicyMaxUpstreamProbes {
			break
		}
		result.AccountsProbed++
		models, fetchErr := fetcher.FetchUpstreamSupportedModels(ctx, account)
		if fetchErr != nil {
			result.AccountsFailed++
			if len(result.Errors) < 10 {
				result.Errors = append(result.Errors, fmt.Sprintf("account %d (%s): %v", account.ID, account.Name, fetchErr))
			}
			continue
		}
		collected = append(collected, models...)
	}
	result.Models = FilterOpenAISiteCandidateModels(collected)
	return result, nil
}
