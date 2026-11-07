package service

import "strings"

// Site policy for OpenAI upstream accounts on this deployment.
// Keep in sync with frontend openaiAllowedModels in useModelWhitelist.ts.
var openAISiteAllowedModels = []string{"gpt-6-astra", "gpt-6.1-sol"}

func openAISiteAllowedModelSet() map[string]struct{} {
	out := make(map[string]struct{}, len(openAISiteAllowedModels))
	for _, id := range openAISiteAllowedModels {
		out[id] = struct{}{}
	}
	return out
}

func openAISiteModelMapping() map[string]any {
	mapping := make(map[string]any, len(openAISiteAllowedModels))
	for _, id := range openAISiteAllowedModels {
		mapping[id] = id
	}
	return mapping
}

func filterOpenAISiteModels(models []string) []string {
	allowed := openAISiteAllowedModelSet()
	out := make([]string, 0, len(openAISiteAllowedModels))
	seen := make(map[string]struct{}, len(openAISiteAllowedModels))
	for _, raw := range models {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// applyOpenAISiteAccountDefaults enforces site model whitelist and Codex image
// strip policy for OpenAI accounts on create/import paths.
func applyOpenAISiteAccountDefaults(platform string, credentials, extra map[string]any) (map[string]any, map[string]any) {
	if platform != PlatformOpenAI {
		return credentials, extra
	}
	if credentials == nil {
		credentials = make(map[string]any)
	}
	credentials["model_mapping"] = openAISiteModelMapping()

	if extra == nil {
		extra = make(map[string]any)
	}
	delete(extra, featureKeyCodexImageGenerationBridge)
	delete(extra, "codex_image_generation_bridge_enabled")
	extra[featureKeyCodexImageGenerationExplicitToolPolicy] = codexImageGenerationExplicitToolPolicyStrip
	return credentials, extra
}
