package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterOpenAISiteModels(t *testing.T) {
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

func TestApplyOpenAISiteAccountDefaults(t *testing.T) {
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
