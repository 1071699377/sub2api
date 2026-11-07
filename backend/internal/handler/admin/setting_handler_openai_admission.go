package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetOpenAIAccountAdmissionSettings 获取新 OpenAI OAuth 账号准入配置
// GET /api/v1/admin/settings/openai-account-admission
func (h *SettingHandler) GetOpenAIAccountAdmissionSettings(c *gin.Context) {
	cfg, err := h.settingService.GetOpenAIAccountAdmissionConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// UpdateOpenAIAccountAdmissionSettings 更新新 OpenAI OAuth 账号准入配置
// PUT /api/v1/admin/settings/openai-account-admission
func (h *SettingHandler) UpdateOpenAIAccountAdmissionSettings(c *gin.Context) {
	var req service.OpenAIAccountAdmissionConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.settingService.SetOpenAIAccountAdmissionConfig(c.Request.Context(), &req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	cfg, err := h.settingService.GetOpenAIAccountAdmissionConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}
