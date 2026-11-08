//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func admissionTestState(pass, iso int) OpenAIAccountAdmissionState {
	p := DefaultOpenAIAccountAdmissionConfig()
	p.PassCount, p.IsolationCount, p.IsolationGroupID = pass, iso, 9
	return OpenAIAccountAdmissionState{Policy: p, Phase: OpenAIAdmissionPhasePending}
}

func TestOpenAIAdmissionClassifyRequiresExactCompleteAnswer(t *testing.T) {
	p := DefaultOpenAIAccountAdmissionConfig()
	require.Equal(t, OpenAIAdmissionOutcomePassed, ClassifyOpenAIAdmissionAnswer(p, true, " 21\n"))
	require.Equal(t, OpenAIAdmissionOutcomeDegraded, ClassifyOpenAIAdmissionAnswer(p, true, "29"))
	for _, text := range []string{"129", "21 或 29", "答案是 21", "", "29。"} {
		require.Equal(t, OpenAIAdmissionOutcomeInconclusive, ClassifyOpenAIAdmissionAnswer(p, true, text), text)
	}
	// 失败的请求即使文本恰好是 21 也不算通过。
	require.Equal(t, OpenAIAdmissionOutcomeInconclusive, ClassifyOpenAIAdmissionAnswer(p, false, "21"))
}

func TestOpenAIAdmissionPassesAfterConsecutiveCorrectAnswers(t *testing.T) {
	now := time.Now()
	s := admissionTestState(3, 3)
	var action string
	for i := 0; i < 2; i++ {
		s, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
		require.Equal(t, OpenAIAdmissionActionObserved, action)
	}
	s, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	require.Equal(t, OpenAIAdmissionActionPassed, action)
	require.Equal(t, OpenAIAdmissionPhasePassed, s.Phase)
	require.NotNil(t, s.ApprovedAt)

	// 通过后再答对不会重复触发放行。
	_, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	require.Equal(t, OpenAIAdmissionActionObserved, action)
}

func TestOpenAIAdmissionInterruptionsResetStreaks(t *testing.T) {
	now := time.Now()
	s := admissionTestState(3, 3)
	s, _ = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	s, _ = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	s, _ = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomeInconclusive, true, now)
	require.Zero(t, s.PassStreak)

	s, _ = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomeDegraded, true, now)
	s, _ = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	require.Zero(t, s.IsolationStreak, "答对会打断隔离计数")
	require.Equal(t, 1, s.PassStreak)
}

func TestOpenAIAdmissionInactiveAccountCannotPass(t *testing.T) {
	now := time.Now()
	s := admissionTestState(1, 3)
	s, action := AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, false, now)
	require.Equal(t, OpenAIAdmissionActionObserved, action)
	require.Equal(t, OpenAIAdmissionPhasePending, s.Phase)
	require.Equal(t, OpenAIAdmissionOutcomeInconclusive, s.LastOutcome)
}

func TestOpenAIAdmissionIsolationIsFinal(t *testing.T) {
	now := time.Now()
	s := admissionTestState(3, 2)
	s, action := AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomeDegraded, true, now)
	require.Equal(t, OpenAIAdmissionActionObserved, action)
	s, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomeDegraded, true, now)
	require.Equal(t, OpenAIAdmissionActionIsolated, action)

	// 仓储把分组不可用的情况标为待隔离；之后答对也只会继续重试隔离。
	s.Phase = OpenAIAdmissionPhaseIsolationPending
	_, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	require.Equal(t, OpenAIAdmissionActionIsolated, action)

	s.Phase = OpenAIAdmissionPhaseIsolated
	_, action = AdvanceOpenAIAccountAdmission(s, OpenAIAdmissionOutcomePassed, true, now)
	require.Equal(t, OpenAIAdmissionActionObserved, action)
}

func TestOpenAIAdmissionNeedsTesting(t *testing.T) {
	s := admissionTestState(1, 1)
	require.True(t, s.NeedsTesting())
	s.Phase = OpenAIAdmissionPhasePassed
	require.False(t, s.NeedsTesting())
	s.Policy.ContinueAfterPass = true
	require.True(t, s.NeedsTesting())
	s.Phase = OpenAIAdmissionPhaseIsolated
	require.False(t, s.NeedsTesting())
	s.Phase = OpenAIAdmissionPhaseIsolationPending
	require.True(t, s.NeedsTesting())
}

func TestOpenAIAdmissionConfigValidation(t *testing.T) {
	c := DefaultOpenAIAccountAdmissionConfig()
	require.NoError(t, c.Validate(), "默认关闭时无需分组")
	c.Enabled = true
	require.Error(t, c.Validate(), "启用必须选择隔离分组")
	c.IsolationGroupID = 3
	require.NoError(t, c.Validate())

	bad := c
	bad.ReasoningEffort = "turbo"
	require.Error(t, bad.Validate())
	bad = c
	bad.DegradedAnswer = bad.PassAnswer
	require.Error(t, bad.Validate())
	bad = c
	bad.WaitSeconds = 0
	require.Error(t, bad.Validate())

	empty := OpenAIAccountAdmissionConfig{}
	empty.Normalize()
	require.Equal(t, "gpt-6.1-sol", empty.ModelID)
	require.Equal(t, "low", empty.ReasoningEffort)
	require.Equal(t, "21", empty.PassAnswer)
	require.Equal(t, "29", empty.DegradedAnswer)
}

func TestOpenAIAdmissionStateRoundTripFromExtra(t *testing.T) {
	_, ok := ParseOpenAIAccountAdmissionState(map[string]any{})
	require.False(t, ok)
	now := time.Now().UTC().Truncate(time.Second)
	in := admissionTestState(6, 6)
	in.EnrolledAt = now
	extra := map[string]any{OpenAIAccountAdmissionExtraKey: in}
	out, ok := ParseOpenAIAccountAdmissionState(extra)
	require.True(t, ok)
	require.Equal(t, in.Phase, out.Phase)
	require.Equal(t, in.Policy, out.Policy)
}

func TestOpenAIAdmissionEnrollmentScope(t *testing.T) {
	parent := int64(1)
	require.True(t, shouldEnrollOpenAIAccountAdmission(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.False(t, shouldEnrollOpenAIAccountAdmission(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}))
	require.False(t, shouldEnrollOpenAIAccountAdmission(&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}))
	require.False(t, shouldEnrollOpenAIAccountAdmission(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent}))
}

func TestOpenAIAdmissionCreateStripsForgedStateWhenSettingsUnavailable(t *testing.T) {
	svc := &adminServiceImpl{}
	acc := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Schedulable: true, Concurrency: 5,
		Extra: map[string]any{OpenAIAccountAdmissionExtraKey: map[string]any{"phase": "passed"}}}
	require.NoError(t, svc.applyOpenAIAccountAdmissionOnCreate(t.Context(), acc))
	_, exists := acc.Extra[OpenAIAccountAdmissionExtraKey]
	require.False(t, exists)
	require.True(t, acc.Schedulable)
	require.Equal(t, 5, acc.Concurrency)
}

func TestOpenAIAdmissionTestOverridesOnlyApplyToBackgroundTests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(background bool) map[string]any {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Set(accountTestBackgroundKey, background)
		c.Set(accountTestAdmissionPromptKey, "题目")
		c.Set(accountTestAdmissionEffortKey, "low")
		payload := createOpenAITestPayload("gpt-6.1-sol", true)
		applyOpenAIAdmissionTestOverrides(c, payload)
		return payload
	}

	p := build(true)
	require.Equal(t, map[string]any{"effort": "low"}, p["reasoning"])
	input := p["input"].([]map[string]any)
	content := input[0]["content"].([]map[string]any)
	require.Equal(t, "题目", content[0]["text"])

	p = build(false)
	_, hasReasoning := p["reasoning"]
	require.False(t, hasReasoning, "手动测试不受影响")
	content = p["input"].([]map[string]any)[0]["content"].([]map[string]any)
	require.Equal(t, "hi", content[0]["text"])
}
