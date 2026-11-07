package service

import (
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/tidwall/gjson"
)

// OpenAISitePolicy 是本站对 OpenAI 平台的全局策略（后台「系统设置 → OpenAI 站点策略」）。
//
//   - AllowedModels：站点允许对外提供的 OpenAI 模型（客户端可见的模型名）。
//     非空时：新建/导入的 OpenAI 账号 model_mapping 被设为该列表；账号同步上游模型、
//     /v1/models 及 Codex 模型清单都只保留列表内模型。为空表示不限制。
//   - ImageGenerationEnabled：全局生图开关。关闭时所有分组（含未绑定分组的 key）
//     的生图请求一律 403，Codex 客户端自带的 image_generation 工具被剥离。
type OpenAISitePolicy struct {
	AllowedModels          []string `json:"allowed_models"`
	ImageGenerationEnabled bool     `json:"image_generation_enabled"`
}

// legacyOpenAISiteAllowedModels 是设置项尚未保存时的默认白名单（保持升级前行为）。
var legacyOpenAISiteAllowedModels = []string{"gpt-6-astra", "gpt-6.1-sol"}

// DefaultOpenAISitePolicy 返回数据库中没有保存过策略时使用的默认值：
// 沿用原先写死的两个模型，生图默认关闭。
func DefaultOpenAISitePolicy() OpenAISitePolicy {
	return OpenAISitePolicy{
		AllowedModels:          append([]string(nil), legacyOpenAISiteAllowedModels...),
		ImageGenerationEnabled: false,
	}
}

// openAISitePolicyStore 保存进程内生效的策略。nil 表示尚未加载（单元测试场景）：
// 此时不限制模型、允许生图，保持上游原始行为。
var openAISitePolicyStore atomic.Pointer[OpenAISitePolicy]

// SetOpenAISitePolicy 更新进程内生效的策略（启动加载、后台保存、定时刷新时调用）。
func SetOpenAISitePolicy(policy OpenAISitePolicy) {
	normalized := NormalizeOpenAISitePolicy(policy)
	openAISitePolicyStore.Store(&normalized)
}

// resetOpenAISitePolicyForTest 恢复为「未加载」状态，供测试清理使用。
func resetOpenAISitePolicyForTest() {
	openAISitePolicyStore.Store(nil)
}

// CurrentOpenAISitePolicy 返回当前生效策略的副本。
func CurrentOpenAISitePolicy() OpenAISitePolicy {
	p := openAISitePolicyStore.Load()
	if p == nil {
		return OpenAISitePolicy{ImageGenerationEnabled: true}
	}
	return OpenAISitePolicy{
		AllowedModels:          append([]string(nil), p.AllowedModels...),
		ImageGenerationEnabled: p.ImageGenerationEnabled,
	}
}

// NormalizeOpenAISitePolicy 去空白、去重（保持首次出现顺序）。
func NormalizeOpenAISitePolicy(policy OpenAISitePolicy) OpenAISitePolicy {
	out := OpenAISitePolicy{ImageGenerationEnabled: policy.ImageGenerationEnabled}
	seen := make(map[string]struct{}, len(policy.AllowedModels))
	for _, raw := range policy.AllowedModels {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out.AllowedModels = append(out.AllowedModels, id)
	}
	if out.AllowedModels == nil {
		out.AllowedModels = []string{}
	}
	return out
}

// OpenAISiteModelsRestricted 报告当前是否启用了站点模型白名单。
func OpenAISiteModelsRestricted() bool {
	p := openAISitePolicyStore.Load()
	return p != nil && len(p.AllowedModels) > 0
}

// OpenAISiteImageGenerationEnabled 报告全局生图开关是否打开。
func OpenAISiteImageGenerationEnabled() bool {
	p := openAISitePolicyStore.Load()
	return p == nil || p.ImageGenerationEnabled
}

// OpenAISiteAllowsModel 判断模型是否在站点白名单内（未启用白名单时恒为 true）。
func OpenAISiteAllowsModel(model string) bool {
	p := openAISitePolicyStore.Load()
	if p == nil || len(p.AllowedModels) == 0 {
		return true
	}
	model = strings.TrimSpace(model)
	for _, id := range p.AllowedModels {
		if id == model {
			return true
		}
	}
	return false
}

func openAISiteAllowedModelSet() map[string]struct{} {
	p := CurrentOpenAISitePolicy()
	out := make(map[string]struct{}, len(p.AllowedModels))
	for _, id := range p.AllowedModels {
		out[id] = struct{}{}
	}
	return out
}

// openAISiteModelMapping 返回把白名单模型映射到自身的 model_mapping；未启用白名单时返回 nil。
func openAISiteModelMapping() map[string]any {
	p := CurrentOpenAISitePolicy()
	if len(p.AllowedModels) == 0 {
		return nil
	}
	mapping := make(map[string]any, len(p.AllowedModels))
	for _, id := range p.AllowedModels {
		mapping[id] = id
	}
	return mapping
}

// filterOpenAISiteModels 去空白、去重，并在启用白名单时只保留白名单内模型。
func filterOpenAISiteModels(models []string) []string {
	restricted := OpenAISiteModelsRestricted()
	allowed := openAISiteAllowedModelSet()
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if restricted {
			if _, ok := allowed[id]; !ok {
				continue
			}
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// FilterOpenAISiteModels 是 filterOpenAISiteModels 的导出版本，供 handler 过滤模型列表。
func FilterOpenAISiteModels(models []string) []string {
	return filterOpenAISiteModels(models)
}

// FilterOpenAISiteCandidateModels 整理「从上游同步」得到的候选模型：去重排序，
// 生图关闭时剔除 gpt-image 等生图模型。不按当前白名单过滤（候选就是用来改白名单的）。
func FilterOpenAISiteCandidateModels(models []string) []string {
	imagesEnabled := OpenAISiteImageGenerationEnabled()
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, raw := range models {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if !imagesEnabled && isOpenAIImageGenerationModel(id) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// FilterOpenAIModelsBodyBySitePolicy 按站点白名单过滤模型清单 JSON：
// OpenAI 风格的 data[].id 与 Codex 风格的 models[].slug 都会被过滤，其余字段原样保留。
// 未启用白名单、或 body 不是可识别的清单时返回 (body, false)。
func FilterOpenAIModelsBodyBySitePolicy(body []byte) ([]byte, bool) {
	if !OpenAISiteModelsRestricted() || len(body) == 0 {
		return body, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	changed := false
	for key, idField := range map[string]string{"data": "id", "models": "slug"} {
		raw, ok := root[key]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			continue
		}
		kept := make([]json.RawMessage, 0, len(items))
		for _, item := range items {
			id := gjson.GetBytes(item, idField).String()
			if id == "" && idField == "slug" {
				id = gjson.GetBytes(item, "id").String()
			}
			if OpenAISiteAllowsModel(id) {
				kept = append(kept, item)
			}
		}
		if len(kept) == len(items) {
			continue
		}
		encoded, err := json.Marshal(kept)
		if err != nil {
			return body, false
		}
		root[key] = encoded
		changed = true
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return out, true
}

// applyOpenAISiteAccountDefaults 在新建/导入 OpenAI 账号时应用站点策略：
// 启用白名单时把 model_mapping 设为白名单；并默认剥离 Codex 生图工具、关闭生图桥接。
func applyOpenAISiteAccountDefaults(platform string, credentials, extra map[string]any) (map[string]any, map[string]any) {
	if platform != PlatformOpenAI {
		return credentials, extra
	}
	if mapping := openAISiteModelMapping(); mapping != nil {
		if credentials == nil {
			credentials = make(map[string]any)
		}
		credentials["model_mapping"] = mapping
	}

	if extra == nil {
		extra = make(map[string]any)
	}
	delete(extra, featureKeyCodexImageGenerationBridge)
	delete(extra, "codex_image_generation_bridge_enabled")
	extra[featureKeyCodexImageGenerationExplicitToolPolicy] = codexImageGenerationExplicitToolPolicyStrip
	return credentials, extra
}
