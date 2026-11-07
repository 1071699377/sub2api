//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 新建一个处于准入中的 OpenAI OAuth 账号（未开放调度、并发 1），返回账号 ID。
func seedAdmissionAccount(t *testing.T, ctx context.Context, origGroup, isoGroup int64, mutate func(*service.OpenAIAccountAdmissionState)) int64 {
	t.Helper()
	policy := service.DefaultOpenAIAccountAdmissionConfig()
	policy.Enabled, policy.PassCount, policy.IsolationCount, policy.IsolationGroupID, policy.WaitSeconds = true, 2, 2, isoGroup, 60
	past := time.Now().UTC().Add(-time.Minute)
	state := service.OpenAIAccountAdmissionState{Policy: policy, Phase: service.OpenAIAdmissionPhasePending, EnrolledAt: past, NextRunAt: &past}
	if mutate != nil {
		mutate(&state)
	}
	acc := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("admission-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Extra: map[string]any{service.OpenAIAccountAdmissionExtraKey: state},
	})
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET schedulable=false, concurrency=1 WHERE id=$1`, acc.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO account_groups(account_id, group_id, priority, created_at) VALUES ($1,$2,1,NOW())`, acc.ID, origGroup)
	require.NoError(t, err)
	return acc.ID
}

func claimAdmission(t *testing.T, ctx context.Context, repo service.OpenAIAccountAdmissionRepository, accountID int64) service.OpenAIAccountAdmissionClaim {
	t.Helper()
	claims, err := repo.ClaimDue(ctx, time.Now().UTC(), 1000, 15*time.Minute)
	require.NoError(t, err)
	for _, c := range claims {
		if c.AccountID == accountID {
			return c
		}
	}
	t.Fatalf("account %d was not claimed", accountID)
	return service.OpenAIAccountAdmissionClaim{}
}

func makeAdmissionDue(t *testing.T, ctx context.Context, accountID int64) {
	t.Helper()
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra = jsonb_set(extra, '{openai_account_admission,next_run_at}', to_jsonb($2::text)) WHERE id=$1`,
		accountID, time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano))
	require.NoError(t, err)
}

func readAdmissionAccount(t *testing.T, ctx context.Context, accountID int64) (service.OpenAIAccountAdmissionState, bool, int, []int64) {
	t.Helper()
	var raw []byte
	var schedulable bool
	var concurrency int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra->'openai_account_admission', schedulable, concurrency FROM accounts WHERE id=$1`, accountID).
		Scan(&raw, &schedulable, &concurrency))
	var state service.OpenAIAccountAdmissionState
	require.NoError(t, json.Unmarshal(raw, &state))
	groups, err := admissionAccountGroupsDB(ctx, accountID)
	require.NoError(t, err)
	return state, schedulable, concurrency, groups
}

func admissionAccountGroupsDB(ctx context.Context, accountID int64) ([]int64, error) {
	rows, err := integrationDB.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id=$1 ORDER BY group_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func TestOpenAIAccountAdmissionRepo_PassFlowOpensScheduling(t *testing.T) {
	ctx := context.Background()
	repo := NewOpenAIAccountAdmissionRepository(integrationDB)
	orig := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("orig-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	iso := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("iso-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	id := seedAdmissionAccount(t, ctx, orig.ID, iso.ID, nil)

	claim := claimAdmission(t, ctx, repo, id)
	require.NotEmpty(t, claim.LeaseToken)

	// 租约期内不会被再次领取。
	again, err := repo.ClaimDue(ctx, time.Now().UTC(), 1000, 15*time.Minute)
	require.NoError(t, err)
	for _, c := range again {
		require.NotEqual(t, id, c.AccountID, "leased account must not be claimed twice")
	}

	action, err := repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomePassed, Answer: "21"}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIAdmissionActionObserved, action)

	// 同一租约重放不会重复计数。
	action, err = repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomePassed}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "lease_lost", action)

	state, schedulable, concurrency, _ := readAdmissionAccount(t, ctx, id)
	require.Equal(t, 1, state.PassStreak)
	require.False(t, schedulable)
	require.Equal(t, 1, concurrency)
	require.NotNil(t, state.NextRunAt)
	require.True(t, state.NextRunAt.After(time.Now()), "next run respects wait_seconds")

	// 未到期不会被领取。
	notDue, err := repo.ClaimDue(ctx, time.Now().UTC(), 1000, 15*time.Minute)
	require.NoError(t, err)
	for _, c := range notDue {
		require.NotEqual(t, id, c.AccountID)
	}

	makeAdmissionDue(t, ctx, id)
	claim = claimAdmission(t, ctx, repo, id)
	action, err = repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomePassed, Answer: "21"}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIAdmissionActionPassed, action)

	state, schedulable, concurrency, groups := readAdmissionAccount(t, ctx, id)
	require.Equal(t, service.OpenAIAdmissionPhasePassed, state.Phase)
	require.True(t, schedulable)
	require.Equal(t, state.Policy.PassedConcurrency, concurrency)
	require.Equal(t, []int64{orig.ID}, groups, "passing keeps original groups")

	var outbox int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type=$2`, id, service.SchedulerOutboxEventAccountChanged).Scan(&outbox))
	require.GreaterOrEqual(t, outbox, 1, "scheduler cache must be notified")

	// 通过后默认不再测试。
	makeAdmissionDue(t, ctx, id)
	after, err := repo.ClaimDue(ctx, time.Now().UTC(), 1000, 15*time.Minute)
	require.NoError(t, err)
	for _, c := range after {
		require.NotEqual(t, id, c.AccountID)
	}
}

func TestOpenAIAccountAdmissionRepo_IsolationMovesGroups(t *testing.T) {
	ctx := context.Background()
	repo := NewOpenAIAccountAdmissionRepository(integrationDB)
	orig := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("orig-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	iso := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("iso-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	id := seedAdmissionAccount(t, ctx, orig.ID, iso.ID, nil)

	for i := 0; i < 2; i++ {
		makeAdmissionDue(t, ctx, id)
		claim := claimAdmission(t, ctx, repo, id)
		_, err := repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomeDegraded, Answer: "29"}, time.Now().UTC())
		require.NoError(t, err)
	}
	state, schedulable, _, groups := readAdmissionAccount(t, ctx, id)
	require.Equal(t, service.OpenAIAdmissionPhaseIsolated, state.Phase)
	require.Equal(t, []int64{iso.ID}, groups)
	require.False(t, schedulable, "never-approved account stays off customer scheduling")
	require.NotNil(t, state.IsolatedAt)

	// 隔离后不再测试。
	makeAdmissionDue(t, ctx, id)
	after, err := repo.ClaimDue(ctx, time.Now().UTC(), 1000, 15*time.Minute)
	require.NoError(t, err)
	for _, c := range after {
		require.NotEqual(t, id, c.AccountID)
	}
}

func TestOpenAIAccountAdmissionRepo_IsolationPendingWhenGroupUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := NewOpenAIAccountAdmissionRepository(integrationDB)
	orig := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("orig-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	iso := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("iso-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	// 已通过且开启了持续测试的账号，之后被降级。
	id := seedAdmissionAccount(t, ctx, orig.ID, iso.ID, func(s *service.OpenAIAccountAdmissionState) {
		s.Phase = service.OpenAIAdmissionPhasePassed
		s.Policy.ContinueAfterPass = true
		s.IsolationStreak = 1
	})
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET schedulable=true WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET status='disabled' WHERE id=$1`, iso.ID)
	require.NoError(t, err)

	claim := claimAdmission(t, ctx, repo, id)
	action, err := repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomeDegraded}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIAdmissionActionIsolationPending, action)
	state, schedulable, _, groups := readAdmissionAccount(t, ctx, id)
	require.Equal(t, service.OpenAIAdmissionPhaseIsolationPending, state.Phase)
	require.False(t, schedulable, "customer traffic is stopped while isolation is pending")
	require.Equal(t, []int64{orig.ID}, groups)
	require.NotNil(t, state.IsolationSchedulable)
	require.True(t, *state.IsolationSchedulable)

	// 分组恢复后完成移动，并还原隔离前的调度开关。
	_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET status='active' WHERE id=$1`, iso.ID)
	require.NoError(t, err)
	makeAdmissionDue(t, ctx, id)
	claim = claimAdmission(t, ctx, repo, id)
	action, err = repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomeInconclusive}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIAdmissionActionIsolated, action)
	state, schedulable, _, groups = readAdmissionAccount(t, ctx, id)
	require.Equal(t, service.OpenAIAdmissionPhaseIsolated, state.Phase)
	require.True(t, schedulable)
	require.Equal(t, []int64{iso.ID}, groups)
}

func TestOpenAIAccountAdmissionRepo_InactiveAccountDoesNotPass(t *testing.T) {
	ctx := context.Background()
	repo := NewOpenAIAccountAdmissionRepository(integrationDB)
	orig := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("orig-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	iso := mustCreateGroup(t, integrationEntClient, &service.Group{Name: fmt.Sprintf("iso-%d", time.Now().UnixNano()), Platform: service.PlatformOpenAI})
	id := seedAdmissionAccount(t, ctx, orig.ID, iso.ID, func(s *service.OpenAIAccountAdmissionState) { s.PassStreak = 1 })
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET status='error' WHERE id=$1`, id)
	require.NoError(t, err)

	claim := claimAdmission(t, ctx, repo, id)
	action, err := repo.ApplyResult(ctx, claim, service.OpenAIAccountAdmissionResult{Outcome: service.OpenAIAdmissionOutcomePassed, Answer: "21"}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, service.OpenAIAdmissionActionObserved, action)
	state, schedulable, _, _ := readAdmissionAccount(t, ctx, id)
	require.Equal(t, service.OpenAIAdmissionPhasePending, state.Phase)
	require.Zero(t, state.PassStreak)
	require.False(t, schedulable)
}
