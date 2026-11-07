package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAIAccountAdmissionRepository struct {
	db *sql.DB
}

func NewOpenAIAccountAdmissionRepository(db *sql.DB) service.OpenAIAccountAdmissionRepository {
	return &openAIAccountAdmissionRepository{db: db}
}

// 准入状态所在的 JSON 路径前缀。
const admissionExtraPath = "extra->'" + service.OpenAIAccountAdmissionExtraKey + "'"

// ClaimDue 用 FOR UPDATE SKIP LOCKED 领取到期账号并写入租约，多实例下同一账号同一时间只会被一个执行器测试。
func (r *openAIAccountAdmissionRepository) ClaimDue(ctx context.Context, now time.Time, limit int, leaseFor time.Duration) ([]service.OpenAIAccountAdmissionClaim, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
SELECT id, `+admissionExtraPath+`
FROM accounts
WHERE deleted_at IS NULL
  AND platform = $1 AND type = $2 AND parent_account_id IS NULL
  AND `+admissionExtraPath+` IS NOT NULL
  AND (
        `+admissionExtraPath+`->>'phase' IN ($3, $4)
     OR (`+admissionExtraPath+`->>'phase' = $5
         AND COALESCE((`+admissionExtraPath+`->'policy'->>'continue_after_pass')::boolean, false))
  )
  AND COALESCE((`+admissionExtraPath+`->>'next_run_at')::timestamptz, '-infinity'::timestamptz) <= $6
  AND COALESCE((`+admissionExtraPath+`->>'lease_until')::timestamptz, '-infinity'::timestamptz) < $6
ORDER BY id
LIMIT $7
FOR UPDATE SKIP LOCKED`,
		service.PlatformOpenAI, service.AccountTypeOAuth,
		service.OpenAIAdmissionPhasePending, service.OpenAIAdmissionPhaseIsolationPending, service.OpenAIAdmissionPhasePassed,
		now, limit)
	if err != nil {
		return nil, err
	}
	type due struct {
		id    int64
		state service.OpenAIAccountAdmissionState
	}
	var dues []due
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var state service.OpenAIAccountAdmissionState
		if err := json.Unmarshal(raw, &state); err != nil {
			continue // 损坏的状态跳过，不阻塞其他账号
		}
		dues = append(dues, due{id: id, state: state})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	leaseUntil := now.Add(leaseFor).UTC()
	claims := make([]service.OpenAIAccountAdmissionClaim, 0, len(dues))
	for _, d := range dues {
		token, err := newAdmissionLeaseToken()
		if err != nil {
			return nil, err
		}
		d.state.LeaseToken = token
		d.state.LeaseUntil = &leaseUntil
		if err := writeAdmissionState(ctx, tx, d.id, d.state); err != nil {
			return nil, err
		}
		claims = append(claims, service.OpenAIAccountAdmissionClaim{AccountID: d.id, LeaseToken: token, State: d.state})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claims, nil
}

// ApplyResult 校验租约后推进状态。租约不匹配（被其他执行器接管或已处理）时不改任何数据。
func (r *openAIAccountAdmissionRepository) ApplyResult(ctx context.Context, claim service.OpenAIAccountAdmissionClaim, result service.OpenAIAccountAdmissionResult, now time.Time) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var raw []byte
	var status string
	var schedulable bool
	err = tx.QueryRowContext(ctx, `
SELECT COALESCE(`+admissionExtraPath+`, 'null'::jsonb), status, schedulable
FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, claim.AccountID).Scan(&raw, &status, &schedulable)
	if err == sql.ErrNoRows {
		return "account_deleted", nil
	}
	if err != nil {
		return "", err
	}
	var state service.OpenAIAccountAdmissionState
	if err := json.Unmarshal(raw, &state); err != nil || state.Phase == "" {
		return "admission_removed", nil
	}
	if state.LeaseToken == "" || state.LeaseToken != claim.LeaseToken {
		return "lease_lost", nil
	}

	state, action := service.AdvanceOpenAIAccountAdmission(state, result.Outcome, status == service.StatusActive, now)
	state.LeaseToken, state.LeaseUntil = "", nil
	state.LastRunAt = &now
	state.LastAnswer = truncateRunes(result.Answer, 64)
	state.LastError = truncateRunes(result.Error, 256)
	next := now.Add(time.Duration(state.Policy.WaitSeconds) * time.Second)
	state.NextRunAt = &next

	setSchedulable := (*bool)(nil)
	setConcurrency := 0
	var groupsBefore []int64
	groupsChanged := false

	switch action {
	case service.OpenAIAdmissionActionPassed:
		v := true
		setSchedulable = &v
		setConcurrency = state.Policy.PassedConcurrency
	case service.OpenAIAdmissionActionIsolated:
		valid, err := admissionGroupUsable(ctx, tx, state.Policy.IsolationGroupID)
		if err != nil {
			return "", err
		}
		if !valid {
			// 目标分组不可用：保留隔离决定，先关闭客户调度，之后每轮重试移动。
			if state.Phase != service.OpenAIAdmissionPhaseIsolationPending {
				prev := schedulable
				state.IsolationSchedulable = &prev
			}
			state.Phase = service.OpenAIAdmissionPhaseIsolationPending
			action = service.OpenAIAdmissionActionIsolationPending
			v := false
			setSchedulable = &v
			break
		}
		if groupsBefore, err = admissionAccountGroups(ctx, tx, claim.AccountID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id = $1`, claim.AccountID); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_groups (account_id, group_id, priority, created_at) VALUES ($1, $2, 1, NOW())`,
			claim.AccountID, state.Policy.IsolationGroupID); err != nil {
			return "", err
		}
		groupsChanged = true
		if state.Phase == service.OpenAIAdmissionPhaseIsolationPending && state.IsolationSchedulable != nil {
			v := *state.IsolationSchedulable
			setSchedulable = &v
		}
		state.Phase = service.OpenAIAdmissionPhaseIsolated
		state.IsolationSchedulable = nil
		state.IsolatedAt = &now
	}

	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	changed := setSchedulable != nil || setConcurrency > 0 || groupsChanged
	var schedArg sql.NullBool
	if setSchedulable != nil {
		schedArg = sql.NullBool{Bool: *setSchedulable, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE accounts SET
  extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{`+service.OpenAIAccountAdmissionExtraKey+`}', $2::jsonb),
  schedulable = COALESCE($3::boolean, schedulable),
  concurrency = CASE WHEN $4::int > 0 THEN $4::int ELSE concurrency END,
  updated_at = CASE WHEN $5::boolean THEN NOW() ELSE updated_at END
WHERE id = $1`, claim.AccountID, string(encoded), schedArg, setConcurrency, changed); err != nil {
		return "", err
	}

	if changed {
		accountID := claim.AccountID
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			return "", err
		}
		if groupsChanged {
			payload := buildSchedulerGroupPayload(mergeGroupIDs(groupsBefore, []int64{state.Policy.IsolationGroupID}))
			if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
				return "", err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return action, nil
}

func writeAdmissionState(ctx context.Context, tx *sql.Tx, accountID int64, state service.OpenAIAccountAdmissionState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE accounts SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{`+service.OpenAIAccountAdmissionExtraKey+`}', $2::jsonb)
WHERE id = $1`, accountID, string(encoded))
	return err
}

func admissionGroupUsable(ctx context.Context, tx *sql.Tx, groupID int64) (bool, error) {
	if groupID <= 0 {
		return false, nil
	}
	var ok bool
	err := tx.QueryRowContext(ctx, `
SELECT platform = $2 AND status = $3 FROM groups WHERE id = $1 AND deleted_at IS NULL FOR SHARE`,
		groupID, service.PlatformOpenAI, service.StatusActive).Scan(&ok)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return ok, err
}

func admissionAccountGroups(ctx context.Context, tx *sql.Tx, accountID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id = $1`, accountID)
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

func newAdmissionLeaseToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate admission lease token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
