package service

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIRequestBodyContainsImageInput(t *testing.T) {
	t.Parallel()

	require.False(t, openAIRequestBodyContainsImageInput([]byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"hi"}]}`)))
	require.True(t, openAIRequestBodyContainsImageInput([]byte(
		`{"model":"gpt-6-astra","messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]},{"role":"user","content":"follow-up"}]}`,
	)))
	require.True(t, openAIRequestBodyContainsImageInput([]byte(
		`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`,
	)))
	require.True(t, openAIRequestBodyContainsImageInput([]byte(
		`{"model":"claude-opus-5","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`,
	)))
	require.Equal(t, openAINonTextMediaAudio, openAIRequestBodyNonTextMediaKind([]byte(
		`{"model":"gpt-6-astra","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`,
	)))
	require.Equal(t, openAINonTextMediaVideo, openAIRequestBodyNonTextMediaKind([]byte(
		`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_video","video_url":"https://example.com/a.mp4"}]}]}`,
	)))
}

func TestRejectOpenAITextOnlyVideoInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	svc := &OpenAIGatewayService{cfg: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/a.mp4"}}]}]}`)
	err := svc.RejectOpenAITextOnlyImageInputChatCompletions(c, PlatformOpenAI, body, "gpt-6-astra")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support video input")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRejectOpenAITextOnlyAudioInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	svc := &OpenAIGatewayService{cfg: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]}`)
	err := svc.RejectOpenAITextOnlyImageInputChatCompletions(c, PlatformOpenAI, body, "gpt-6-astra")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support audio input")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRejectOpenAITextOnlyLive(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	svc := &OpenAIGatewayService{cfg: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	err := svc.RejectOpenAITextOnlyLive(c, PlatformOpenAI)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Live/realtime voice is disabled")
}

func TestAllowSmallInlineImageUnderTextOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	cfg.Gateway.MaxInboundImageBytes = 1 << 20
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{Platform: PlatformOpenAI}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`)
	require.NoError(t, svc.rejectOpenAITextOnlyImageInput(c, account, body, "gpt-6-astra"))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRejectOversizedInlineImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	cfg.Gateway.MaxInboundImageBytes = 1024
	svc := &OpenAIGatewayService{cfg: cfg}

	raw := strings.Repeat("A", 2048)
	b64 := base64.StdEncoding.EncodeToString([]byte(raw))
	body := []byte(fmt.Sprintf(
		`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]}]}`,
		b64,
	))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	err := svc.RejectOpenAITextOnlyImageInputChatCompletions(c, PlatformOpenAI, body, "gpt-6.1-sol")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "rejects images larger than 1024 bytes")
}

func TestRejectOversizedAnthropicInlineImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	cfg.Gateway.MaxInboundImageBytes = 100
	svc := &OpenAIGatewayService{cfg: cfg}

	b64 := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("B", 200)))
	body := []byte(fmt.Sprintf(
		`{"model":"claude-opus-5","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":%q}}]}]}`,
		b64,
	))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	err := svc.RejectTextOnlyMediaInputAnthropic(c, body, "claude-opus-5")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "rejects images larger than 100 bytes")
}

func TestAllowRemoteImageURLUnderTextOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	cfg.Gateway.MaxInboundImageBytes = 1 << 20
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{Platform: PlatformOpenAI}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`)
	require.NoError(t, svc.rejectOpenAITextOnlyImageInput(c, account, body, "gpt-6-astra"))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRejectTextOnlyMediaEndpointAllowsImage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.OpenAITextOnly = true
	svc := &OpenAIGatewayService{cfg: cfg}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.NoError(t, svc.RejectTextOnlyMediaEndpoint(c, TextOnlyMediaImage))
	require.Equal(t, http.StatusOK, w.Code)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	err := svc.RejectTextOnlyMediaEndpoint(c2, TextOnlyMediaVideo)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w2.Code)
}

func TestEstimateBase64DecodedBytes(t *testing.T) {
	t.Parallel()
	require.Equal(t, int64(3), estimateBase64DecodedBytes(base64.StdEncoding.EncodeToString([]byte("abc"))))
	require.Equal(t, int64(1), estimateBase64DecodedBytes("YQ=="))
}
