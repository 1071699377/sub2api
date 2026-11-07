package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	// SettingKeyOpenAISiteAllowedModels 站点 OpenAI 模型白名单（JSON 字符串数组；[] 表示不限制）。
	SettingKeyOpenAISiteAllowedModels = "openai_site_allowed_models"
	// SettingKeyOpenAISiteImageGenerationEnabled 站点全局生图开关（"true"/"false"）。
	SettingKeyOpenAISiteImageGenerationEnabled = "openai_site_image_generation_enabled"

	// openAISitePolicyRefreshInterval 多实例部署时其他实例保存的策略在此间隔内生效。
	openAISitePolicyRefreshInterval = 30 * time.Second
	openAISitePolicyMaxModels       = 500
)

var openAISitePolicyRefresherOnce sync.Once

// GetOpenAISitePolicy 从数据库读取站点策略；未保存过的字段使用 DefaultOpenAISitePolicy。
func (s *SettingService) GetOpenAISitePolicy(ctx context.Context) (OpenAISitePolicy, error) {
	policy := DefaultOpenAISitePolicy()
	if s == nil || s.settingRepo == nil {
		return policy, nil
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyOpenAISiteAllowedModels,
		SettingKeyOpenAISiteImageGenerationEnabled,
	})
	if err != nil {
		return policy, fmt.Errorf("get openai site policy: %w", err)
	}
	if raw, ok := values[SettingKeyOpenAISiteAllowedModels]; ok {
		raw = strings.TrimSpace(raw)
		models := []string{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &models); err != nil {
				return policy, fmt.Errorf("parse %s setting: %w", SettingKeyOpenAISiteAllowedModels, err)
			}
		}
		policy.AllowedModels = models
	}
	if raw, ok := values[SettingKeyOpenAISiteImageGenerationEnabled]; ok {
		policy.ImageGenerationEnabled = strings.TrimSpace(raw) == "true"
	}
	return NormalizeOpenAISitePolicy(policy), nil
}

// UpdateOpenAISitePolicy 校验并保存站点策略，保存成功后立即在本进程生效。
func (s *SettingService) UpdateOpenAISitePolicy(ctx context.Context, policy OpenAISitePolicy) (OpenAISitePolicy, error) {
	policy = NormalizeOpenAISitePolicy(policy)
	if len(policy.AllowedModels) > openAISitePolicyMaxModels {
		return policy, fmt.Errorf("allowed_models cannot exceed %d entries", openAISitePolicyMaxModels)
	}
	for _, id := range policy.AllowedModels {
		if len(id) > 200 || strings.ContainsAny(id, " \t\r\n") {
			return policy, fmt.Errorf("invalid model id %q", id)
		}
	}
	if s == nil || s.settingRepo == nil {
		return policy, errors.New("setting repository is not configured")
	}
	encoded, err := json.Marshal(policy.AllowedModels)
	if err != nil {
		return policy, fmt.Errorf("marshal allowed models: %w", err)
	}
	imageValue := "false"
	if policy.ImageGenerationEnabled {
		imageValue = "true"
	}
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyOpenAISiteAllowedModels:          string(encoded),
		SettingKeyOpenAISiteImageGenerationEnabled: imageValue,
	}); err != nil {
		return policy, fmt.Errorf("save openai site policy: %w", err)
	}
	SetOpenAISitePolicy(policy)
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return policy, nil
}

// LoadOpenAISitePolicy 启动时加载站点策略，并启动后台定时刷新（多实例同步）。
// 读取失败时使用默认策略（白名单两款模型、关闭生图），宁严勿松。
func (s *SettingService) LoadOpenAISitePolicy(ctx context.Context) error {
	policy, err := s.GetOpenAISitePolicy(ctx)
	SetOpenAISitePolicy(policy)
	openAISitePolicyRefresherOnce.Do(func() {
		go s.runOpenAISitePolicyRefresher()
	})
	return err
}

func (s *SettingService) runOpenAISitePolicyRefresher() {
	ticker := time.NewTicker(openAISitePolicyRefreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		policy, err := s.GetOpenAISitePolicy(ctx)
		cancel()
		if err != nil {
			// 刷新失败保留上次成功加载的策略。
			slog.Warn("openai_site_policy_refresh_failed", "error", err)
			continue
		}
		SetOpenAISitePolicy(policy)
	}
}
