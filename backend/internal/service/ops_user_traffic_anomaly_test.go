package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAggregateUserTrafficMetricsCurrentMinute(t *testing.T) {
	end := time.Date(2026, 11, 6, 12, 10, 0, 0, time.UTC)
	buckets := []UserTrafficMinuteBucket{
		{UserID: 1, Email: "a@x", Bucket: end.Add(-2 * time.Minute), Requests: 5},
		{UserID: 1, Email: "a@x", Bucket: end.Add(-1 * time.Minute), Requests: 40},
		{UserID: 1, Email: "a@x", Bucket: end.Add(-3 * time.Minute), Requests: 2},
		{UserID: 2, Email: "b@x", Bucket: end.Add(-1 * time.Minute), Requests: 3},
	}
	metrics := aggregateUserTrafficMetrics(buckets, end)
	require.Len(t, metrics, 2)
	byID := map[int64]userTrafficMetrics{}
	for _, m := range metrics {
		byID[m.UserID] = m
	}
	require.Equal(t, 40.0, byID[1].Current)
	require.Equal(t, 40.0, byID[1].Max)
	require.Equal(t, 2.0, byID[1].Min)
	require.InDelta(t, (5+40+2)/3.0, byID[1].Avg, 0.001)
	require.Equal(t, 3.0, byID[2].Current)
}

func TestEvaluateUserTrafficAnomaliesZScore(t *testing.T) {
	metrics := make([]userTrafficMetrics, 0, 12)
	for i := 1; i <= 11; i++ {
		metrics = append(metrics, userTrafficMetrics{UserID: int64(i), Current: 2, Max: 3, Min: 1, Avg: 2})
	}
	metrics = append(metrics, userTrafficMetrics{UserID: 99, Current: 80, Max: 80, Min: 10, Avg: 40})
	summary, zByUser := computePopulationTrafficStats(metrics)
	require.GreaterOrEqual(t, summary.ActiveUsers, 10)
	items := evaluateUserTrafficAnomalies(metrics, summary, zByUser, nil)
	require.NotEmpty(t, items)
	found := false
	for _, it := range items {
		if it.UserID == 99 {
			found = true
			require.Contains(t, it.Reasons, "z_score")
		}
	}
	require.True(t, found)
}

func TestEvaluateUserTrafficAnomaliesConcurrency(t *testing.T) {
	items := evaluateUserTrafficAnomalies(
		nil,
		populationTrafficStats{},
		nil,
		map[int64]*UserConcurrencyInfo{
			7: {UserID: 7, UserEmail: "c@x", Username: "c", LoadPercentage: 95, WaitingInQueue: 1, CurrentInUse: 9, MaxCapacity: 10},
		},
	)
	require.Len(t, items, 1)
	require.Equal(t, int64(7), items[0].UserID)
	require.Contains(t, items[0].Reasons, "high_concurrency")
}

func TestApplyUserTrafficAnomalySticky(t *testing.T) {
	userTrafficAnomalyHitsMu.Lock()
	userTrafficAnomalyHits = map[int64]int{}
	userTrafficAnomalyHitsMu.Unlock()

	cands := []UserTrafficAnomalyItem{{UserID: 1, Current: 50, Reasons: []string{"z_score"}}}

	first := applyUserTrafficAnomalySticky(cands)
	require.Len(t, first, 1)
	require.Equal(t, 1, first[0].HitCount)
	require.False(t, first[0].Confirmed)

	second := applyUserTrafficAnomalySticky(cands)
	require.Len(t, second, 1)
	require.Equal(t, 2, second[0].HitCount)
	require.True(t, second[0].Confirmed)

	// Absence clears sticky state.
	cleared := applyUserTrafficAnomalySticky(nil)
	require.Empty(t, cleared)
	again := applyUserTrafficAnomalySticky(cands)
	require.Len(t, again, 1)
	require.Equal(t, 1, again[0].HitCount)
	require.False(t, again[0].Confirmed)
}
