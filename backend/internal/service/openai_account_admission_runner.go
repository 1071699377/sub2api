package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// OpenAIAccountAdmissionClaim 一次领取到的待测账号（已加租约）。
type OpenAIAccountAdmissionClaim struct {
	AccountID  int64
	LeaseToken string
	State      OpenAIAccountAdmissionState
}

// OpenAIAccountAdmissionResult 一轮测试的结果摘要。
type OpenAIAccountAdmissionResult struct {
	Outcome string
	Answer  string
	Error   string
}

// OpenAIAccountAdmissionRepository 准入持久化边界。
// ClaimDue 原子领取到期账号并写入租约；ApplyResult 在同一事务内校验租约、
// 推进状态、调整调度开关/并发/分组并通知调度缓存。
type OpenAIAccountAdmissionRepository interface {
	ClaimDue(ctx context.Context, now time.Time, limit int, leaseFor time.Duration) ([]OpenAIAccountAdmissionClaim, error)
	ApplyResult(ctx context.Context, claim OpenAIAccountAdmissionClaim, result OpenAIAccountAdmissionResult, now time.Time) (string, error)
}

const (
	openAIAdmissionBatchSize  = 50
	openAIAdmissionMaxWorkers = 5
	openAIAdmissionTestTimeout = 3 * time.Minute
)

// runOpenAIAccountAdmission 每分钟由 ScheduledTestRunnerService 调用。
func (s *ScheduledTestRunnerService) runOpenAIAccountAdmission() {
	if s == nil || s.admissionRepo == nil || s.accountTestSvc == nil {
		return
	}
	if !s.admissionRunning.CompareAndSwap(false, true) {
		return // 上一轮仍在执行
	}
	defer s.admissionRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	claims, err := s.admissionRepo.ClaimDue(ctx, time.Now().UTC(), openAIAdmissionBatchSize, openAIAdmissionLease)
	if err != nil {
		logger.LegacyPrintf("service.openai_admission", "[OpenAIAdmission] claim due accounts failed: %v", err)
		return
	}
	if len(claims) == 0 {
		return
	}

	sem := make(chan struct{}, openAIAdmissionMaxWorkers)
	var wg sync.WaitGroup
	for _, claim := range claims {
		sem <- struct{}{}
		wg.Add(1)
		go func(c OpenAIAccountAdmissionClaim) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOneOpenAIAccountAdmission(ctx, c)
		}(claim)
	}
	wg.Wait()
}

func (s *ScheduledTestRunnerService) runOneOpenAIAccountAdmission(ctx context.Context, claim OpenAIAccountAdmissionClaim) {
	policy := claim.State.Policy
	result := OpenAIAccountAdmissionResult{Outcome: OpenAIAdmissionOutcomeInconclusive}

	// 待隔离账号不再测试，只重试移动分组。
	if claim.State.Phase != OpenAIAdmissionPhaseIsolationPending {
		testCtx, cancel := context.WithTimeout(ctx, openAIAdmissionTestTimeout)
		res, err := s.accountTestSvc.RunOpenAIAdmissionTestBackground(testCtx, claim.AccountID, policy.ModelID, OpenAIAdmissionCandyPrompt, policy.ReasoningEffort)
		cancel()
		switch {
		case err != nil:
			result.Error = err.Error()
		case res != nil:
			result.Answer = res.ResponseText
			result.Error = res.ErrorMessage
			result.Outcome = ClassifyOpenAIAdmissionAnswer(policy, res.Status == "success", res.ResponseText)
		}
	}

	action, err := s.admissionRepo.ApplyResult(ctx, claim, result, time.Now().UTC())
	if err != nil {
		logger.LegacyPrintf("service.openai_admission", "[OpenAIAdmission] account=%d apply result failed: %v", claim.AccountID, err)
		return
	}
	if action != OpenAIAdmissionActionObserved {
		logger.LegacyPrintf("service.openai_admission", "[OpenAIAdmission] account=%d outcome=%s action=%s", claim.AccountID, result.Outcome, action)
	}
}
