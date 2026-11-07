package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const defaultMaxInboundImageBytes int64 = 1 << 20 // 1 MiB decoded

func (s *OpenAIGatewayService) openAITextOnlyEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAITextOnly
}

func (s *OpenAIGatewayService) maxInboundImageBytes() int64 {
	if s != nil && s.cfg != nil && s.cfg.Gateway.MaxInboundImageBytes > 0 {
		return s.cfg.Gateway.MaxInboundImageBytes
	}
	return defaultMaxInboundImageBytes
}

// openAINonTextMediaKind describes which non-text modality was found.
type openAINonTextMediaKind string

const (
	openAINonTextMediaNone  openAINonTextMediaKind = ""
	openAINonTextMediaImage openAINonTextMediaKind = "image"
	openAINonTextMediaAudio openAINonTextMediaKind = "audio"
	openAINonTextMediaVideo openAINonTextMediaKind = "video"
)

// Exported aliases for handlers that reject dedicated media endpoints.
const (
	TextOnlyMediaImage openAINonTextMediaKind = openAINonTextMediaImage
	TextOnlyMediaAudio openAINonTextMediaKind = openAINonTextMediaAudio
	TextOnlyMediaVideo openAINonTextMediaKind = openAINonTextMediaVideo
)

type openAIMediaPolicyViolation struct {
	Kind          openAINonTextMediaKind
	ImageBytes    int64
	ImageLimit    int64
	OversizedImg  bool
}

func (v openAIMediaPolicyViolation) Message(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "unknown"
	}
	if v.OversizedImg {
		return fmt.Sprintf(
			"model %q rejects images larger than %d bytes (got %d bytes)",
			model, v.ImageLimit, v.ImageBytes,
		)
	}
	switch v.Kind {
	case openAINonTextMediaAudio:
		return fmt.Sprintf("model %q does not support audio input", model)
	case openAINonTextMediaVideo:
		return fmt.Sprintf("model %q does not support video input", model)
	default:
		return fmt.Sprintf("model %q does not support image input", model)
	}
}

// openAIRequestBodyContainsImageInput scans the full input/messages tree for
// vision parts. Prefer this over openAIRequestBodyMayContainImageInput when
// rejecting multimodal requests: the latter only inspects the last message.
func openAIRequestBodyContainsImageInput(body []byte) bool {
	return openAIRequestBodyNonTextMediaKind(body) == openAINonTextMediaImage
}

func openAIRequestBodyNonTextMediaKind(body []byte) openAINonTextMediaKind {
	if len(body) == 0 {
		return openAINonTextMediaNone
	}
	if kind := openAIJSONValueNonTextMediaKind(gjson.GetBytes(body, "input")); kind != openAINonTextMediaNone {
		return kind
	}
	return openAIJSONValueNonTextMediaKind(gjson.GetBytes(body, "messages"))
}

func openAIJSONValueNonTextMediaKind(value gjson.Result) openAINonTextMediaKind {
	if !value.Exists() {
		return openAINonTextMediaNone
	}
	if value.IsArray() {
		found := openAINonTextMediaNone
		value.ForEach(func(_, item gjson.Result) bool {
			if kind := openAIJSONValueNonTextMediaKind(item); kind != openAINonTextMediaNone {
				found = kind
				return false
			}
			return true
		})
		return found
	}
	if value.IsObject() {
		partType := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch {
		case partType == "input_image",
			partType == "image", // Anthropic Messages / Claude Code
			value.Get("image_url").Exists():
			return openAINonTextMediaImage
		case partType == "input_audio",
			partType == "audio",
			partType == "audio_url",
			value.Get("input_audio").Exists(),
			value.Get("audio_url").Exists(),
			value.Get("audio").Exists():
			return openAINonTextMediaAudio
		case partType == "input_video",
			partType == "video",
			partType == "video_url",
			value.Get("input_video").Exists(),
			value.Get("video_url").Exists(),
			value.Get("video").Exists():
			return openAINonTextMediaVideo
		}
		return openAIJSONValueNonTextMediaKind(value.Get("content"))
	}
	return openAINonTextMediaNone
}

func estimateBase64DecodedBytes(b64 string) int64 {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return 0
	}
	// Strip whitespace/newlines that clients sometimes embed.
	if strings.IndexByte(b64, '\n') >= 0 || strings.IndexByte(b64, '\r') >= 0 || strings.IndexByte(b64, ' ') >= 0 {
		b64 = strings.Map(func(r rune) rune {
			switch r {
			case '\n', '\r', ' ', '\t':
				return -1
			default:
				return r
			}
		}, b64)
	}
	n := int64(len(b64))
	pad := int64(0)
	if n >= 1 && b64[n-1] == '=' {
		pad++
	}
	if n >= 2 && b64[n-2] == '=' {
		pad++
	}
	decoded := n/4*3 - pad
	if decoded < 0 {
		return 0
	}
	return decoded
}

func extractInlineImageBase64(value gjson.Result) (string, bool) {
	if !value.IsObject() {
		return "", false
	}
	partType := strings.ToLower(strings.TrimSpace(value.Get("type").String()))

	// Anthropic: {"type":"image","source":{"type":"base64","data":"..."}}
	if partType == "image" {
		src := value.Get("source")
		if strings.EqualFold(src.Get("type").String(), "base64") {
			if data := src.Get("data").String(); data != "" {
				return data, true
			}
		}
	}

	// OpenAI chat: image_url may be string or {"url":"data:image/...;base64,..."}
	url := ""
	if partType == "input_image" || value.Get("image_url").Exists() {
		imageURL := value.Get("image_url")
		if imageURL.IsObject() {
			url = imageURL.Get("url").String()
		} else if imageURL.Type == gjson.String {
			url = imageURL.String()
		}
	}
	if url == "" && partType == "input_image" {
		url = value.Get("url").String()
	}
	if idx := strings.Index(url, "base64,"); idx >= 0 {
		return url[idx+len("base64,"):], true
	}
	return "", false
}

func findOversizedInlineImage(value gjson.Result, limit int64) (int64, bool) {
	if limit <= 0 || !value.Exists() {
		return 0, false
	}
	if value.IsArray() {
		var found int64
		hit := false
		value.ForEach(func(_, item gjson.Result) bool {
			if size, ok := findOversizedInlineImage(item, limit); ok {
				found = size
				hit = true
				return false
			}
			return true
		})
		return found, hit
	}
	if value.IsObject() {
		if b64, ok := extractInlineImageBase64(value); ok {
			size := estimateBase64DecodedBytes(b64)
			if size > limit {
				return size, true
			}
		}
		if size, ok := findOversizedInlineImage(value.Get("content"), limit); ok {
			return size, true
		}
		if size, ok := findOversizedInlineImage(value.Get("output"), limit); ok {
			return size, true
		}
	}
	return 0, false
}

func openAIRequestBodyOversizedInlineImage(body []byte, limit int64) (int64, bool) {
	if len(body) == 0 || limit <= 0 {
		return 0, false
	}
	if size, ok := findOversizedInlineImage(gjson.GetBytes(body, "input"), limit); ok {
		return size, true
	}
	return findOversizedInlineImage(gjson.GetBytes(body, "messages"), limit)
}

func openAITextOnlyMediaInputMessage(model string, kind openAINonTextMediaKind) string {
	return openAIMediaPolicyViolation{Kind: kind}.Message(model)
}

// evaluateMediaInputPolicy applies current gateway media rules:
// - openai_text_only: reject audio/video; allow images subject to max size
// - always enforce MaxInboundImageBytes on inline/base64 images when configured (>0)
func (s *OpenAIGatewayService) evaluateMediaInputPolicy(body []byte) (openAIMediaPolicyViolation, bool) {
	if s == nil {
		return openAIMediaPolicyViolation{}, false
	}
	textOnly := s.openAITextOnlyEnabled()
	limit := s.maxInboundImageBytes()

	kind := openAIRequestBodyNonTextMediaKind(body)
	if textOnly && (kind == openAINonTextMediaAudio || kind == openAINonTextMediaVideo) {
		return openAIMediaPolicyViolation{Kind: kind}, true
	}

	// Images are allowed; enforce per-image decoded size for inline/base64 payloads.
	if size, ok := openAIRequestBodyOversizedInlineImage(body, limit); ok {
		return openAIMediaPolicyViolation{
			Kind:         openAINonTextMediaImage,
			ImageBytes:   size,
			ImageLimit:   limit,
			OversizedImg: true,
		}, true
	}
	return openAIMediaPolicyViolation{}, false
}

func (s *OpenAIGatewayService) shouldRejectOpenAITextOnlyMediaInput(platform string, body []byte) (openAINonTextMediaKind, bool) {
	_ = platform
	v, reject := s.evaluateMediaInputPolicy(body)
	if !reject {
		return openAINonTextMediaNone, false
	}
	return v.Kind, true
}

func (s *OpenAIGatewayService) rejectMediaInputPolicy(c *gin.Context, body []byte, model string, write func(msg string)) error {
	v, reject := s.evaluateMediaInputPolicy(body)
	if !reject {
		return nil
	}
	msg := v.Message(firstNonEmptyString(model, gjson.GetBytes(body, "model").String()))
	if write != nil {
		write(msg)
	}
	return errors.New(msg)
}

// RejectOpenAITextOnlyImageInput writes a 400 invalid_request_error when media
// policy is violated (audio/video banned under text-only, or oversized image).
func (s *OpenAIGatewayService) RejectOpenAITextOnlyImageInput(c *gin.Context, platform string, body []byte, model string) error {
	_ = platform
	return s.rejectMediaInputPolicy(c, body, model, func(msg string) {
		if c == nil {
			return
		}
		setOpsUpstreamError(c, http.StatusBadRequest, msg, "")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": msg,
				"param":   "input",
			},
		})
	})
}

// RejectOpenAITextOnlyImageInputChatCompletions mirrors RejectOpenAITextOnlyImageInput
// but uses the Chat Completions error envelope.
func (s *OpenAIGatewayService) RejectOpenAITextOnlyImageInputChatCompletions(c *gin.Context, platform string, body []byte, model string) error {
	_ = platform
	return s.rejectMediaInputPolicy(c, body, model, func(msg string) {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", msg)
	})
}

// RejectOpenAITextOnlyLive rejects Frameless Live / realtime voice calls when
// gateway.openai_text_only is enabled.
func (s *OpenAIGatewayService) RejectOpenAITextOnlyLive(c *gin.Context, platform string) error {
	_ = platform
	if !s.openAITextOnlyEnabled() {
		return nil
	}
	msg := "Live/realtime voice is disabled while gateway.openai_text_only is enabled"
	if c != nil {
		setOpsUpstreamError(c, http.StatusBadRequest, msg, "")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": msg,
			},
		})
	}
	return errors.New(msg)
}

// RejectTextOnlyMediaInputAnthropic rejects Anthropic Messages media that
// violates the current gateway policy.
func (s *OpenAIGatewayService) RejectTextOnlyMediaInputAnthropic(c *gin.Context, body []byte, model string) error {
	return s.rejectMediaInputPolicy(c, body, model, func(msg string) {
		if c == nil {
			return
		}
		setOpsUpstreamError(c, http.StatusBadRequest, msg, "")
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": msg},
		})
	})
}

// RejectTextOnlyMediaEndpoint rejects dedicated audio/video API endpoints when
// gateway.openai_text_only is enabled. Image endpoints are allowed.
func (s *OpenAIGatewayService) RejectTextOnlyMediaEndpoint(c *gin.Context, kind openAINonTextMediaKind) error {
	if !s.openAITextOnlyEnabled() {
		return nil
	}
	if kind == openAINonTextMediaImage || kind == openAINonTextMediaNone {
		return nil
	}
	model := "unknown"
	msg := openAITextOnlyMediaInputMessage(model, kind)
	if c != nil {
		setOpsUpstreamError(c, http.StatusBadRequest, msg, "")
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", msg)
	}
	return errors.New(msg)
}

func (s *OpenAIGatewayService) rejectOpenAITextOnlyImageInput(c *gin.Context, account *Account, body []byte, model string) error {
	platform := ""
	if account != nil {
		platform = account.Platform
	}
	return s.RejectOpenAITextOnlyImageInput(c, platform, body, model)
}

func (s *OpenAIGatewayService) rejectOpenAITextOnlyImageInputChatCompletions(c *gin.Context, account *Account, body []byte, model string) error {
	platform := ""
	if account != nil {
		platform = account.Platform
	}
	return s.RejectOpenAITextOnlyImageInputChatCompletions(c, platform, body, model)
}

// openAITextOnlyImageInputWSError returns a policy-violation close error when
// media policy blocks a WebSocket response.create payload.
func (s *OpenAIGatewayService) openAITextOnlyImageInputWSError(account *Account, body []byte, model string) error {
	_ = account
	v, reject := s.evaluateMediaInputPolicy(body)
	if !reject {
		return nil
	}
	msg := v.Message(firstNonEmptyString(model, gjson.GetBytes(body, "model").String()))
	return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, msg, errors.New(msg))
}
