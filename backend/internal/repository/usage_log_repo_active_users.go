package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

const activeUsersRangeMaxDuration = 31 * 24 * time.Hour

// GetActiveUsersInRange returns distinct active users and request counts for [start, end).
// Hourly buckets are truncated in Asia/Shanghai for display.
func (r *usageLogRepository) GetActiveUsersInRange(ctx context.Context, start, end time.Time) (*usagestats.ActiveUsersRangeStats, error) {
	if !end.After(start) {
		return nil, fmt.Errorf("end must be after start")
	}
	if end.Sub(start) > activeUsersRangeMaxDuration {
		return nil, fmt.Errorf("time range must be <= 31 days")
	}

	out := &usagestats.ActiveUsersRangeStats{
		Start:  start,
		End:    end,
		Hourly: []usagestats.ActiveUsersHourlyPoint{},
	}

	summaryQuery := `
		SELECT
			COUNT(DISTINCT user_id) AS active_users,
			COUNT(*) AS requests
		FROM usage_logs
		WHERE created_at >= $1
			AND created_at < $2
			AND user_id IS NOT NULL
	`
	if err := scanSingleRow(ctx, r.sql, summaryQuery, []any{start, end}, &out.ActiveUsers, &out.Requests); err != nil {
		return nil, fmt.Errorf("active users summary: %w", err)
	}

	hourlyQuery := `
		SELECT
			date_trunc('hour', created_at AT TIME ZONE 'Asia/Shanghai') AS hour_bj,
			COUNT(DISTINCT user_id) AS active_users,
			COUNT(*) AS requests
		FROM usage_logs
		WHERE created_at >= $1
			AND created_at < $2
			AND user_id IS NOT NULL
		GROUP BY 1
		ORDER BY 1
	`
	rows, err := r.sql.QueryContext(ctx, hourlyQuery, start, end)
	if err != nil {
		return nil, fmt.Errorf("active users hourly: %w", err)
	}
	defer rows.Close()

	loc, locErr := time.LoadLocation("Asia/Shanghai")
	if locErr != nil {
		loc = time.FixedZone("CST", 8*3600)
	}

	for rows.Next() {
		var point usagestats.ActiveUsersHourlyPoint
		var hour time.Time
		if err := rows.Scan(&hour, &point.ActiveUsers, &point.Requests); err != nil {
			return nil, fmt.Errorf("scan active users hourly: %w", err)
		}
		// date_trunc AT TIME ZONE returns timestamp without tz; pin to Shanghai.
		point.Hour = time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour(), 0, 0, 0, loc)
		out.Hourly = append(out.Hourly, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active users hourly: %w", err)
	}
	return out, nil
}
