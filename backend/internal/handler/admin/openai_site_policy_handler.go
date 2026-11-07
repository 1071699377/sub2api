package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// openAISitePolicyResponse 是 OpenAI 站点策略接口的返回体。
type openAISitePolicyResponse struct {
	AllowedModels          []string `json:"allowed_models"`
	ImageGenerationEnabled bool     `json:"image_generation_enabled"`
	// DefaultModels 是内置的 OpenAI 模型目录，前端在「手动选择」时作为候选。
	DefaultModels []string `json:"default_models"`
}

type updateOpenAISitePolicyRequest struct {
	AllowedModels          []string `json:"allowed_models"`
	ImageGenerationEnabled bool     `json:"image_generation_enabled"`
}

func buildOpenAISitePolicyResponse(policy service.OpenAISitePolicy) openAISitePolicyResponse {
	allowed := policy.AllowedModels
	if allowed == nil {
		allowed = []string{}
	}
	return openAISitePolicyResponse{
		AllowedModels:          allowed,
		ImageGenerationEnabled: policy.ImageGenerationEnabled,
		DefaultModels:          service.FilterOpenAISiteCandidateModels(openai.DefaultModelIDs()),
	}
}

// GetOpenAISitePolicy 获取 OpenAI 站点策略（模型白名单 + 全局生图开关）
// GET /api/v1/admin/settings/openai-site-policy
func (h *SettingHandler) GetOpenAISitePolicy(c *gin.Context) {
	policy, err := h.settingService.GetOpenAISitePolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, buildOpenAISitePolicyResponse(policy))
}

// UpdateOpenAISitePolicy 更新 OpenAI 站点策略
// PUT /api/v1/admin/settings/openai-site-policy
func (h *SettingHandler) UpdateOpenAISitePolicy(c *gin.Context) {
	var req updateOpenAISitePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	policy, err := h.settingService.UpdateOpenAISitePolicy(c.Request.Context(), service.OpenAISitePolicy{
		AllowedModels:          req.AllowedModels,
		ImageGenerationEnabled: req.ImageGenerationEnabled,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, buildOpenAISitePolicyResponse(policy))
}

// ApplyOpenAISitePolicyToAccounts 把当前站点白名单写入所有现有 OpenAI 账号
// POST /api/v1/admin/settings/openai-site-policy/apply-to-accounts
func (h *AccountHandler) ApplyOpenAISitePolicyToAccounts(c *gin.Context) {
	result, err := service.ApplyOpenAISiteModelsToAccounts(c.Request.Context(), h.adminService)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// SyncOpenAISitePolicyUpstreamModels 从现有 OpenAI 账号的上游拉取模型列表，作为白名单候选
// POST /api/v1/admin/settings/openai-site-policy/sync-upstream
func (h *AccountHandler) SyncOpenAISitePolicyUpstreamModels(c *gin.Context) {
	if h.accountTestService == nil {
		response.InternalError(c, "Account test service is not configured")
		return
	}
	result, err := service.CollectOpenAISiteUpstreamModels(c.Request.Context(), h.adminService, h.accountTestService)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.AccountsProbed > 0 && result.AccountsFailed == result.AccountsProbed {
		response.Error(c, http.StatusBadGateway, "Failed to fetch upstream models from all OpenAI accounts")
		return
	}
	response.Success(c, result)
}
