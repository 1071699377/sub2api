package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ListUserTrafficMinuteBuckets aggregates usage_logs by user_id and minute bucket
// within [start, end). Joins users for email/username.
func (r *opsRepository) ListUserTrafficMinuteBuckets(ctx context.Context, start, end time.Time) ([]service.UserTrafficMinuteBucket, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if start.IsZero() || end.IsZero() {
		return nil, fmt.Errorf("start/end required")
	}
	start = start.UTC()
	end = end.UTC()
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}
	if end.Sub(start) > time.Hour {
		return nil, fmt.Errorf("window too large")
	}

	q := `
SELECT
  ul.user_id,
  COALESCE(u.email, ''),
  COALESCE(u.username, ''),
  date_trunc('minute', ul.created_at) AS bucket,
  COUNT(*)::bigint AS requests,
  COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0)::bigint AS tokens,
  COALESCE(SUM(ul.actual_cost), 0)::float8 AS actual_cost
FROM usage_logs ul
LEFT JOIN users u ON u.id = ul.user_id
WHERE ul.created_at >= $1 AND ul.created_at < $2
  AND ul.user_id IS NOT NULL
GROUP BY 1, 2, 3, 4
ORDER BY 1, 4`

	rows, err := r.db.QueryContext(ctx, q, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.UserTrafficMinuteBucket, 0, 128)
	for rows.Next() {
		var b service.UserTrafficMinuteBucket
		if err := rows.Scan(&b.UserID, &b.Email, &b.Username, &b.Bucket, &b.Requests, &b.Tokens, &b.ActualCost); err != nil {
			return nil, err
		}
		b.Bucket = b.Bucket.UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}
