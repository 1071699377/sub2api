-- Daily check-in ledger used by sub2api-checkin sidecar.
CREATE TABLE IF NOT EXISTS user_daily_checkins (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period_key  DATE NOT NULL,
    amount      NUMERIC(20, 8) NOT NULL DEFAULT 50,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, period_key)
);

CREATE INDEX IF NOT EXISTS idx_user_daily_checkins_user
    ON user_daily_checkins (user_id);
