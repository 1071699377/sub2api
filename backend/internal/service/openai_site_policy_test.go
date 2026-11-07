package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// withOpenAISitePolicy 在测试期间设置站点策略，结束后恢复为未加载状态。
func withOpenAISitePolicy(t *testing.T, policy OpenAISitePolicy) {
	t.Helper()
	SetOpenAISitePolicy(policy)
	t.Cleanup(resetOpenAISitePolicyForTest)
}

func TestOpenAISitePolicyUnloadedIsUnrestricted(t *testing.T) {
	resetOpenAISitePolicyForTest()
	require.False(t, OpenAISiteModelsRestricted())
	require.True(t, OpenAISiteImageGenerationEnabled())
	require.True(t, OpenAISiteAllowsModel("anything"))
	require.Nil(t, openAISiteModelMapping())
	require.Equal(t, []string{"a", "b"}, filterOpenAISiteModels([]string{" a ", "b", "a", ""}))
}

func TestFilterOpenAISiteModels(t *testing.T) {
	withOpenAISitePolicy(t, DefaultOpenAISitePolicy())
	got := filterOpenAISiteModels([]string{
		"gpt-6-sol",
		"gpt-6-astra",
		"gpt-6.1-sol",
		"gpt-6-astra",
		"codex-auto-review",
		"  gpt-6.1-sol  ",
	})
	require.Equal(t, []string{"gpt-6-astra", "gpt-6.1-sol"}, got)
}

func TestOpenAISitePolicyCustomAllowedModels(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{AllowedModels: []string{" gpt-6-sol ", "gpt-6-sol", "gpt-5.6"}})
	policy := CurrentOpenAISitePolicy()
	require.Equal(t, []string{"gpt-6-sol", "gpt-5.6"}, policy.AllowedModels)
	require.False(t, policy.ImageGenerationEnabled)
	require.True(t, OpenAISiteAllowsModel("gpt-5.6"))
	require.False(t, OpenAISiteAllowsModel("gpt-6-astra"))
	require.Equal(t, map[string]any{"gpt-6-sol": "gpt-6-sol", "gpt-5.6": "gpt-5.6"}, openAISiteModelMapping())
}

func TestOpenAISitePolicyEmptyListMeansUnrestricted(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{AllowedModels: []string{}, ImageGenerationEnabled: true})
	require.False(t, OpenAISiteModelsRestricted())
	require.True(t, OpenAISiteAllowsModel("gpt-6-astra"))
	require.Nil(t, openAISiteModelMapping())
}

func TestApplyOpenAISiteAccountDefaults(t *testing.T) {
	withOpenAISitePolicy(t, DefaultOpenAISitePolicy())
	creds, extra := applyOpenAISiteAccountDefaults(PlatformOpenAI, map[string]any{
		"model_mapping": map[string]any{"gpt-6-sol": "gpt-6-sol"},
	}, map[string]any{
		"codex_image_generation_bridge": true,
	})
	require.Equal(t, openAISiteModelMapping(), creds["model_mapping"])
	require.Equal(t, codexImageGenerationExplicitToolPolicyStrip, extra[featureKeyCodexImageGenerationExplicitToolPolicy])
	_, hasBridge := extra[featureKeyCodexImageGenerationBridge]
	require.False(t, hasBridge)

	creds2, extra2 := applyOpenAISiteAccountDefaults(PlatformAnthropic, map[string]any{"k": "v"}, nil)
	require.Equal(t, map[string]any{"k": "v"}, creds2)
	require.Nil(t, extra2)
}

func TestApplyOpenAISiteAccountDefaultsUnrestrictedKeepsMapping(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{AllowedModels: []string{}})
	original := map[string]any{"gpt-6-sol": "gpt-6-sol"}
	creds, _ := applyOpenAISiteAccountDefaults(PlatformOpenAI, map[string]any{"model_mapping": original}, nil)
	require.Equal(t, original, creds["model_mapping"])
}

func TestOpenAISiteImageGenerationDisabledBlocksAllGroups(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{ImageGenerationEnabled: false})
	require.False(t, GroupAllowsImageGeneration(nil))
	require.False(t, GroupAllowsImageGeneration(&Group{AllowImageGeneration: true}))

	account := &Account{Platform: PlatformOpenAI}
	require.Equal(t, codexImageGenerationExplicitToolPolicyStrip, account.CodexImageGenerationExplicitToolPolicy())
}

func TestOpenAISiteImageGenerationEnabledRespectsGroupFlag(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{ImageGenerationEnabled: true})
	require.True(t, GroupAllowsImageGeneration(nil))
	require.True(t, GroupAllowsImageGeneration(&Group{AllowImageGeneration: true}))
	require.False(t, GroupAllowsImageGeneration(&Group{AllowImageGeneration: false}))

	account := &Account{Platform: PlatformOpenAI}
	require.Equal(t, codexImageGenerationExplicitToolPolicyAllow, account.CodexImageGenerationExplicitToolPolicy())
}

func TestFilterOpenAISiteCandidateModelsDropsImageModelsWhenDisabled(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{ImageGenerationEnabled: false})
	got := FilterOpenAISiteCandidateModels([]string{"gpt-image-2", "gpt-6-sol", "gpt-6-astra", "gpt-6-sol"})
	require.Equal(t, []string{"gpt-6-astra", "gpt-6-sol"}, got)

	SetOpenAISitePolicy(OpenAISitePolicy{ImageGenerationEnabled: true})
	got = FilterOpenAISiteCandidateModels([]string{"gpt-image-2", "gpt-6-sol"})
	require.Equal(t, []string{"gpt-6-sol", "gpt-image-2"}, got)
}

func TestFilterOpenAIModelsBodyBySitePolicy(t *testing.T) {
	withOpenAISitePolicy(t, OpenAISitePolicy{AllowedModels: []string{"gpt-6-astra"}})

	listBody := []byte(`{"object":"list","data":[{"id":"gpt-6-astra","object":"model"},{"id":"gpt-6-sol","object":"model"}]}`)
	filtered, changed := FilterOpenAIModelsBodyBySitePolicy(listBody)
	require.True(t, changed)
	require.Equal(t, "list", gjson.GetBytes(filtered, "object").String())
	require.Equal(t, int64(1), gjson.GetBytes(filtered, "data.#").Int())
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(filtered, "data.0.id").String())

	manifest := []byte(`{"models":[{"slug":"gpt-6-sol"},{"slug":"gpt-6-astra","display_name":"Astra"}]}`)
	filtered, changed = FilterOpenAIModelsBodyBySitePolicy(manifest)
	require.True(t, changed)
	require.Equal(t, int64(1), gjson.GetBytes(filtered, "models.#").Int())
	require.Equal(t, "Astra", gjson.GetBytes(filtered, "models.0.display_name").String())

	same := []byte(`{"models":[{"slug":"gpt-6-astra"}]}`)
	out, changed := FilterOpenAIModelsBodyBySitePolicy(same)
	require.False(t, changed)
	require.Equal(t, same, out)

	SetOpenAISitePolicy(OpenAISitePolicy{AllowedModels: []string{}})
	out, changed = FilterOpenAIModelsBodyBySitePolicy(listBody)
	require.False(t, changed)
	require.Equal(t, listBody, out)
}
