package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type stubActiveUsersRepo struct {
	UsageLogRepository
	stats *usagestats.ActiveUsersRangeStats
	err   error
}

func (s *stubActiveUsersRepo) GetActiveUsersInRange(_ context.Context, start, end time.Time) (*usagestats.ActiveUsersRangeStats, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.stats != nil {
		return s.stats, nil
	}
	return &usagestats.ActiveUsersRangeStats{
		ActiveUsers: 3,
		Requests:    10,
		Start:       start,
		End:         end,
		Hourly:      []usagestats.ActiveUsersHourlyPoint{},
	}, nil
}

func TestDashboardService_GetActiveUsersInRange(t *testing.T) {
	svc := &DashboardService{usageRepo: &stubActiveUsersRepo{}}
	start := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	got, err := svc.GetActiveUsersInRange(context.Background(), start, end)
	require.NoError(t, err)
	require.Equal(t, int64(3), got.ActiveUsers)
	require.Equal(t, int64(10), got.Requests)
}

func TestDashboardService_GetActiveUsersInRange_UnsupportedRepo(t *testing.T) {
	svc := &DashboardService{usageRepo: &usageBatchLogRepoStub{}}
	_, err := svc.GetActiveUsersInRange(context.Background(), time.Now(), time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestDashboardService_GetActiveUsersInRange_PropagatesValidation(t *testing.T) {
	svc := &DashboardService{usageRepo: &stubActiveUsersRepo{err: errors.New("time range must be <= 31 days")}}
	_, err := svc.GetActiveUsersInRange(context.Background(), time.Now(), time.Now().Add(time.Hour))
	require.Error(t, err)
	require.Contains(t, err.Error(), "31 days")
}
