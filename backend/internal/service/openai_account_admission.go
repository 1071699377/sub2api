package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 新 OpenAI OAuth 账号准入：账号创建后先关闭客户调度、并发固定为 1，
// 后台按固定间隔用同一道推理题测试；连续给出正确答案后开放调度，
// 连续给出降级模型的典型错误答案后移入隔离分组。
//
// 设计参考 ranxi2001/sub2api 38b29a1c9（"为新 OpenAI 2FA 账号提供可配置准入与隔离"），
// 但基于本仓库已有的账号测试链路独立实现，不依赖其质量运维 / 鹈鹕测试子系统。

const (
	// SettingKeyOpenAIAccountAdmission 全局模板（JSON）。
	SettingKeyOpenAIAccountAdmission = "openai_account_admission"
	// OpenAIAccountAdmissionExtraKey 账号级状态（含创建时的配置快照）。
	OpenAIAccountAdmissionExtraKey = "openai_account_admission"

	OpenAIAdmissionPhasePending          = "pending"
	OpenAIAdmissionPhasePassed           = "passed"
	OpenAIAdmissionPhaseIsolationPending = "isolation_pending"
	OpenAIAdmissionPhaseIsolated         = "isolated"

	OpenAIAdmissionOutcomePassed       = "passed"
	OpenAIAdmissionOutcomeDegraded     = "degraded"
	OpenAIAdmissionOutcomeInconclusive = "inconclusive"

	OpenAIAdmissionActionObserved         = "observed"
	OpenAIAdmissionActionPassed           = "admission_passed"
	OpenAIAdmissionActionIsolated         = "admission_isolated"
	OpenAIAdmissionActionIsolationPending = "admission_isolation_pending"

	openAIAdmissionLease = 15 * time.Minute
)

// OpenAIAdmissionCandyPrompt 与参考实现相同的糖果题。正确答案为 21；
// 被降级的账号常见回答 29。
const OpenAIAdmissionCandyPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4

只输出最终数字，不要输出任何解释。`

// OpenAIAccountAdmissionConfig 全局模板。每个新账号登记时复制一份快照，
// 之后修改全局模板不影响已登记账号。
type OpenAIAccountAdmissionConfig struct {
	Enabled           bool   `json:"enabled"`
	ModelID           string `json:"model_id"`
	ReasoningEffort   string `json:"reasoning_effort"`
	WaitSeconds       int    `json:"wait_seconds"`
	PassCount         int    `json:"pass_count"`
	PassedConcurrency int    `json:"passed_concurrency"`
	IsolationCount    int    `json:"isolation_count"`
	IsolationGroupID  int64  `json:"isolation_group_id"`
	// ContinueAfterPass 通过后是否继续后台测试（用于发现之后才被降级的账号）。
	// 默认关闭：持续测试会长期消耗账号额度。
	ContinueAfterPass bool   `json:"continue_after_pass"`
	PassAnswer        string `json:"pass_answer"`
	DegradedAnswer    string `json:"degraded_answer"`
}

func DefaultOpenAIAccountAdmissionConfig() OpenAIAccountAdmissionConfig {
	return OpenAIAccountAdmissionConfig{
		ModelID:           "gpt-6.1-sol",
		ReasoningEffort:   "low",
		WaitSeconds:       60,
		PassCount:         6,
		PassedConcurrency: 2,
		IsolationCount:    6,
		PassAnswer:        "21",
		DegradedAnswer:    "29",
	}
}

var openAIAdmissionReasoningEfforts = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {},
}

// Normalize 补齐缺省值并清理空白，不改变显式配置。
func (c *OpenAIAccountAdmissionConfig) Normalize() {
	d := DefaultOpenAIAccountAdmissionConfig()
	c.ModelID = strings.TrimSpace(c.ModelID)
	c.ReasoningEffort = strings.ToLower(strings.TrimSpace(c.ReasoningEffort))
	c.PassAnswer = strings.TrimSpace(c.PassAnswer)
	c.DegradedAnswer = strings.TrimSpace(c.DegradedAnswer)
	if c.ModelID == "" {
		c.ModelID = d.ModelID
	}
	if c.ReasoningEffort == "" {
		c.ReasoningEffort = d.ReasoningEffort
	}
	if c.PassAnswer == "" {
		c.PassAnswer = d.PassAnswer
	}
	if c.DegradedAnswer == "" {
		c.DegradedAnswer = d.DegradedAnswer
	}
	// 0 对这些字段都不是合法值，视为未填写（便于只提交部分字段）；负数仍交给 Validate 拒绝。
	if c.WaitSeconds == 0 {
		c.WaitSeconds = d.WaitSeconds
	}
	if c.PassCount == 0 {
		c.PassCount = d.PassCount
	}
	if c.PassedConcurrency == 0 {
		c.PassedConcurrency = d.PassedConcurrency
	}
	if c.IsolationCount == 0 {
		c.IsolationCount = d.IsolationCount
	}
}

func (c OpenAIAccountAdmissionConfig) Validate() error {
	switch {
	case len(c.ModelID) > 100 || strings.ContainsAny(c.ModelID, " \t\r\n*"):
		return errors.New("model_id 无效")
	case !isValidOpenAIAdmissionEffort(c.ReasoningEffort):
		return errors.New("reasoning_effort 无效")
	case c.WaitSeconds < 1 || c.WaitSeconds > 86400:
		return errors.New("wait_seconds 须在 1-86400 之间")
	case c.PassCount < 1 || c.PassCount > 1000:
		return errors.New("pass_count 须在 1-1000 之间")
	case c.PassedConcurrency < 1 || c.PassedConcurrency > 10000:
		return errors.New("passed_concurrency 须在 1-10000 之间")
	case c.IsolationCount < 1 || c.IsolationCount > 1000:
		return errors.New("isolation_count 须在 1-1000 之间")
	case c.IsolationGroupID < 0:
		return errors.New("isolation_group_id 无效")
	case c.Enabled && c.IsolationGroupID == 0:
		return errors.New("启用前须选择隔离目标分组")
	case len(c.PassAnswer) > 32 || len(c.DegradedAnswer) > 32:
		return errors.New("答案长度不能超过 32")
	case c.PassAnswer == c.DegradedAnswer:
		return errors.New("通过答案与隔离答案不能相同")
	}
	return nil
}

func isValidOpenAIAdmissionEffort(effort string) bool {
	_, ok := openAIAdmissionReasoningEfforts[effort]
	return ok
}

// OpenAIAccountAdmissionState 账号级准入状态，保存在 accounts.extra。
type OpenAIAccountAdmissionState struct {
	Policy          OpenAIAccountAdmissionConfig `json:"policy"`
	Phase           string                       `json:"phase"`
	PassStreak      int                          `json:"pass_streak"`
	IsolationStreak int                          `json:"isolation_streak"`
	Rounds          int                          `json:"rounds"`
	// IsolationSchedulable 记录进入"待隔离"前的调度开关，目标分组恢复后还原。
	IsolationSchedulable *bool      `json:"isolation_schedulable,omitempty"`
	EnrolledAt           time.Time  `json:"enrolled_at"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	IsolatedAt           *time.Time `json:"isolated_at,omitempty"`
	NextRunAt            *time.Time `json:"next_run_at,omitempty"`
	LeaseUntil           *time.Time `json:"lease_until,omitempty"`
	LeaseToken           string     `json:"lease_token,omitempty"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	LastOutcome          string     `json:"last_outcome,omitempty"`
	LastAnswer           string     `json:"last_answer,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
}

// NeedsTesting 返回该状态是否仍需后台测试。
func (s OpenAIAccountAdmissionState) NeedsTesting() bool {
	switch s.Phase {
	case OpenAIAdmissionPhasePending, OpenAIAdmissionPhaseIsolationPending:
		return true
	case OpenAIAdmissionPhasePassed:
		return s.Policy.ContinueAfterPass
	}
	return false
}

// ClassifyOpenAIAdmissionAnswer 只认可完整成功且去除首尾空白后恰好等于配置答案的回复。
func ClassifyOpenAIAdmissionAnswer(policy OpenAIAccountAdmissionConfig, success bool, text string) string {
	if !success {
		return OpenAIAdmissionOutcomeInconclusive
	}
	answer := strings.TrimSpace(text)
	switch answer {
	case policy.PassAnswer:
		return OpenAIAdmissionOutcomePassed
	case policy.DegradedAnswer:
		return OpenAIAdmissionOutcomeDegraded
	}
	return OpenAIAdmissionOutcomeInconclusive
}

// AdvanceOpenAIAccountAdmission 纯状态机：根据一轮结果推进状态并返回需要执行的动作。
// accountActive=false 时，"通过"不计数（暂停或出错的账号不能自动获得客户调度）。
func AdvanceOpenAIAccountAdmission(s OpenAIAccountAdmissionState, outcome string, accountActive bool, now time.Time) (OpenAIAccountAdmissionState, string) {
	s.Rounds++
	s.LastOutcome = outcome
	if s.Phase == OpenAIAdmissionPhaseIsolationPending {
		// 隔离决定一经作出不可撤销，仅重试移动分组。
		return s, OpenAIAdmissionActionIsolated
	}
	if s.Phase == OpenAIAdmissionPhaseIsolated {
		return s, OpenAIAdmissionActionObserved
	}
	if outcome == OpenAIAdmissionOutcomePassed && !accountActive {
		outcome = OpenAIAdmissionOutcomeInconclusive
		s.LastOutcome = outcome
	}
	switch outcome {
	case OpenAIAdmissionOutcomePassed:
		s.IsolationStreak = 0
		if s.Phase == OpenAIAdmissionPhasePending {
			s.PassStreak++
			if s.PassStreak >= s.Policy.PassCount {
				s.Phase = OpenAIAdmissionPhasePassed
				s.ApprovedAt = &now
				return s, OpenAIAdmissionActionPassed
			}
		}
	case OpenAIAdmissionOutcomeDegraded:
		s.PassStreak = 0
		s.IsolationStreak++
		if s.IsolationStreak >= s.Policy.IsolationCount {
			return s, OpenAIAdmissionActionIsolated
		}
	default:
		// 报错、超时、其他答案都打断两个连续计数。
		s.PassStreak, s.IsolationStreak = 0, 0
	}
	return s, OpenAIAdmissionActionObserved
}

// ParseOpenAIAccountAdmissionState 从账号 extra 读取状态；不存在时返回 ok=false。
func ParseOpenAIAccountAdmissionState(extra map[string]any) (OpenAIAccountAdmissionState, bool) {
	var state OpenAIAccountAdmissionState
	raw, ok := extra[OpenAIAccountAdmissionExtraKey]
	if !ok || raw == nil {
		return state, false
	}
	data, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(data, &state) != nil || state.Phase == "" {
		return OpenAIAccountAdmissionState{}, false
	}
	return state, true
}

// ---------------------------------------------------------------------------
// 全局模板存取
// ---------------------------------------------------------------------------

func (s *SettingService) GetOpenAIAccountAdmissionConfig(ctx context.Context) (*OpenAIAccountAdmissionConfig, error) {
	cfg := DefaultOpenAIAccountAdmissionConfig()
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIAccountAdmission)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("get openai account admission settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return &cfg, nil
	}
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		// 损坏的配置按关闭处理，避免误把新号全部关进准入。
		d := DefaultOpenAIAccountAdmissionConfig()
		return &d, nil
	}
	cfg.Normalize()
	return &cfg, nil
}

func (s *SettingService) SetOpenAIAccountAdmissionConfig(ctx context.Context, cfg *OpenAIAccountAdmissionConfig) error {
	if cfg == nil {
		return errors.New("settings cannot be nil")
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.IsolationGroupID > 0 {
		if err := s.validateOpenAIAdmissionGroup(ctx, cfg.IsolationGroupID); err != nil {
			return err
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal openai account admission settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyOpenAIAccountAdmission, string(data))
}

func (s *SettingService) validateOpenAIAdmissionGroup(ctx context.Context, groupID int64) error {
	if s.defaultSubGroupReader == nil {
		return errors.New("分组校验不可用")
	}
	group, err := s.defaultSubGroupReader.GetByID(ctx, groupID)
	if err != nil || group == nil {
		return errors.New("隔离目标分组不存在")
	}
	if group.Platform != PlatformOpenAI || group.Status != StatusActive {
		return errors.New("隔离目标分组必须是启用中的 OpenAI 分组")
	}
	return nil
}

// ---------------------------------------------------------------------------
// 账号创建时登记
// ---------------------------------------------------------------------------

// shouldEnrollOpenAIAccountAdmission 只对新建的真实 OpenAI OAuth 账号登记。
func shouldEnrollOpenAIAccountAdmission(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth && !account.IsShadow()
}

// applyOpenAIAccountAdmissionOnCreate 在账号落库前调用。
// 无论是否启用都会清掉客户端提交的伪造状态；启用时关闭调度、并发设为 1 并写入状态。
func (s *adminServiceImpl) applyOpenAIAccountAdmissionOnCreate(ctx context.Context, account *Account) error {
	if account == nil {
		return nil
	}
	delete(account.Extra, OpenAIAccountAdmissionExtraKey)
	if !shouldEnrollOpenAIAccountAdmission(account) || s.settingService == nil {
		return nil
	}
	cfg, err := s.settingService.GetOpenAIAccountAdmissionConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if err := s.settingService.validateOpenAIAdmissionGroup(ctx, cfg.IsolationGroupID); err != nil {
		return fmt.Errorf("新账号准入配置无效：%w", err)
	}
	now := time.Now().UTC()
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[OpenAIAccountAdmissionExtraKey] = OpenAIAccountAdmissionState{
		Policy:     *cfg,
		Phase:      OpenAIAdmissionPhasePending,
		EnrolledAt: now,
		NextRunAt:  &now,
	}
	account.Schedulable = false
	account.Concurrency = 1
	return nil
}
