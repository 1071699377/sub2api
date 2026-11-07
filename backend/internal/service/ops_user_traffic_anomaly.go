package service

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

const (
	userTrafficAnomalyCacheTTL       = 10 * time.Second
	userTrafficAnomalyZThreshold     = 3.5
	userTrafficAnomalyAbsoluteFloor  = 10.0 // req/min
	userTrafficAnomalyConfirmHits    = 2
	userTrafficAnomalySmallSampleMax = 10
)

type userTrafficAnomalyCacheEntry struct {
	expiresAt time.Time
	response  *UserTrafficAnomalyResponse
}

var (
	userTrafficAnomalyCacheMu sync.Mutex
	userTrafficAnomalyCache   userTrafficAnomalyCacheEntry
	userTrafficAnomalyHitsMu  sync.Mutex
	userTrafficAnomalyHits    = map[int64]int{}
)

type userTrafficMetrics struct {
	UserID   int64
	Email    string
	Username string
	Current  float64
	Max      float64
	Min      float64
	Avg      float64
	Buckets  int
}

// GetUserTrafficAnomaly returns abnormal users for the observation window (default 5m).
func (s *OpsService) GetUserTrafficAnomaly(ctx context.Context, window time.Duration, windowLabel string) (*UserTrafficAnomalyResponse, error) {
	if s == nil || s.opsRepo == nil {
		return nil, ErrOpsDisabled
	}
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if !s.IsRealtimeMonitoringEnabled(ctx) {
		now := time.Now().UTC()
		return &UserTrafficAnomalyResponse{
			Enabled:   false,
			Timestamp: now,
			Summary: UserTrafficAnomalySummary{
				Window:    windowLabel,
				StartTime: now.Add(-window),
				EndTime:   now,
			},
			Items: []UserTrafficAnomalyItem{},
		}, nil
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	if window > time.Hour {
		window = time.Hour
	}
	if windowLabel == "" {
		windowLabel = "5min"
	}

	now := time.Now().UTC()
	userTrafficAnomalyCacheMu.Lock()
	if userTrafficAnomalyCache.response != nil && now.Before(userTrafficAnomalyCache.expiresAt) {
		cached := cloneUserTrafficAnomalyResponse(userTrafficAnomalyCache.response)
		userTrafficAnomalyCacheMu.Unlock()
		return cached, nil
	}
	userTrafficAnomalyCacheMu.Unlock()

	end := now.Truncate(time.Minute)
	start := end.Add(-window)
	buckets, err := s.opsRepo.ListUserTrafficMinuteBuckets(ctx, start, end)
	if err != nil {
		return nil, err
	}

	metrics := aggregateUserTrafficMetrics(buckets, end)
	summary, zByUser := computePopulationTrafficStats(metrics)

	concurrencyByUser := map[int64]*UserConcurrencyInfo{}
	if conc, _, concErr := s.GetUserConcurrencyStats(ctx); concErr == nil && conc != nil {
		concurrencyByUser = conc
	}

	candidates := evaluateUserTrafficAnomalies(metrics, summary, zByUser, concurrencyByUser)
	items := applyUserTrafficAnomalySticky(candidates)

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].HitCount != items[j].HitCount {
			return items[i].HitCount > items[j].HitCount
		}
		if items[i].ZScore != items[j].ZScore {
			return items[i].ZScore > items[j].ZScore
		}
		return items[i].Current > items[j].Current
	})

	resp := &UserTrafficAnomalyResponse{
		Enabled:   true,
		Timestamp: now,
		Summary: UserTrafficAnomalySummary{
			Window:      windowLabel,
			StartTime:   start,
			EndTime:     end,
			ActiveUsers: summary.ActiveUsers,
			Max:         summary.Max,
			Min:         summary.Min,
			Avg:         summary.Avg,
			Median:      summary.Median,
			MAD:         summary.MAD,
		},
		Items: items,
	}

	userTrafficAnomalyCacheMu.Lock()
	userTrafficAnomalyCache = userTrafficAnomalyCacheEntry{
		expiresAt: now.Add(userTrafficAnomalyCacheTTL),
		response:  cloneUserTrafficAnomalyResponse(resp),
	}
	userTrafficAnomalyCacheMu.Unlock()

	return resp, nil
}

func aggregateUserTrafficMetrics(buckets []UserTrafficMinuteBucket, windowEnd time.Time) []userTrafficMetrics {
	type agg struct {
		email, username string
		byMinute        map[int64]int64
	}
	byUser := map[int64]*agg{}
	for _, b := range buckets {
		a := byUser[b.UserID]
		if a == nil {
			a = &agg{email: b.Email, username: b.Username, byMinute: map[int64]int64{}}
			byUser[b.UserID] = a
		}
		if a.email == "" {
			a.email = b.Email
		}
		if a.username == "" {
			a.username = b.Username
		}
		key := b.Bucket.UTC().Unix()
		a.byMinute[key] += b.Requests
	}

	// Current = most recent complete minute before windowEnd.
	currentMinute := windowEnd.Add(-time.Minute).Unix()
	out := make([]userTrafficMetrics, 0, len(byUser))
	for userID, a := range byUser {
		if len(a.byMinute) == 0 {
			continue
		}
		var sum int64
		var maxV int64
		minV := int64(math.MaxInt64)
		for _, v := range a.byMinute {
			sum += v
			if v > maxV {
				maxV = v
			}
			if v < minV {
				minV = v
			}
		}
		current := float64(a.byMinute[currentMinute])
		n := float64(len(a.byMinute))
		out = append(out, userTrafficMetrics{
			UserID:   userID,
			Email:    a.email,
			Username: a.username,
			Current:  current,
			Max:      float64(maxV),
			Min:      float64(minV),
			Avg:      float64(sum) / n,
			Buckets:  len(a.byMinute),
		})
	}
	return out
}

type populationTrafficStats struct {
	ActiveUsers int
	Max         float64
	Min         float64
	Avg         float64
	Median      float64
	MAD         float64
}

func computePopulationTrafficStats(metrics []userTrafficMetrics) (populationTrafficStats, map[int64]float64) {
	zByUser := map[int64]float64{}
	if len(metrics) == 0 {
		return populationTrafficStats{}, zByUser
	}
	currents := make([]float64, len(metrics))
	var sum float64
	maxV := metrics[0].Current
	minV := metrics[0].Current
	for i, m := range metrics {
		currents[i] = m.Current
		sum += m.Current
		if m.Current > maxV {
			maxV = m.Current
		}
		if m.Current < minV {
			minV = m.Current
		}
	}
	sort.Float64s(currents)
	median := percentileSorted(currents, 0.5)
	devs := make([]float64, len(currents))
	for i, v := range currents {
		devs[i] = math.Abs(v - median)
	}
	sort.Float64s(devs)
	mad := percentileSorted(devs, 0.5)
	for _, m := range metrics {
		zByUser[m.UserID] = robustZScore(m.Current, median, mad)
	}
	return populationTrafficStats{
		ActiveUsers: len(metrics),
		Max:         maxV,
		Min:         minV,
		Avg:         sum / float64(len(metrics)),
		Median:      median,
		MAD:         mad,
	}, zByUser
}

func robustZScore(x, median, mad float64) float64 {
	if mad > 0 {
		return (x - median) / (1.4826 * mad)
	}
	if median == 0 {
		if x == 0 {
			return 0
		}
		return x // absolute when no baseline
	}
	return x / median
}

func percentileSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	w := idx - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}

func evaluateUserTrafficAnomalies(
	metrics []userTrafficMetrics,
	summary populationTrafficStats,
	zByUser map[int64]float64,
	concurrency map[int64]*UserConcurrencyInfo,
) []UserTrafficAnomalyItem {
	floor := math.Max(userTrafficAnomalyAbsoluteFloor, summary.Avg)
	out := make([]UserTrafficAnomalyItem, 0)
	seen := map[int64]struct{}{}

	for _, m := range metrics {
		reasons := make([]string, 0, 3)
		z := zByUser[m.UserID]
		if m.Current >= floor && z >= userTrafficAnomalyZThreshold {
			reasons = append(reasons, "z_score")
		}
		if summary.ActiveUsers < userTrafficAnomalySmallSampleMax &&
			summary.Max > 0 &&
			m.Current >= summary.Max*0.8 &&
			m.Current >= userTrafficAnomalyAbsoluteFloor {
			reasons = append(reasons, "near_population_max")
		}
		if len(reasons) == 0 {
			continue
		}
		item := UserTrafficAnomalyItem{
			UserID:   m.UserID,
			Email:    m.Email,
			Username: m.Username,
			Current:  m.Current,
			Max:      m.Max,
			Min:      m.Min,
			Avg:      m.Avg,
			ZScore:   z,
			Reasons:  reasons,
		}
		if c := concurrency[m.UserID]; c != nil {
			item.CurrentInUse = c.CurrentInUse
			item.MaxCapacity = c.MaxCapacity
			item.LoadPercentage = c.LoadPercentage
			item.WaitingInQueue = c.WaitingInQueue
		}
		out = append(out, item)
		seen[m.UserID] = struct{}{}
	}

	// Concurrency-only anomalies (long streams not yet in usage_logs).
	for userID, c := range concurrency {
		if c == nil {
			continue
		}
		if _, ok := seen[userID]; ok {
			// already included; still attach concurrency reason if overloaded
			continue
		}
		if c.LoadPercentage < 90 && c.WaitingInQueue <= 0 {
			continue
		}
		item := UserTrafficAnomalyItem{
			UserID:         userID,
			Email:          c.UserEmail,
			Username:       c.Username,
			CurrentInUse:   c.CurrentInUse,
			MaxCapacity:    c.MaxCapacity,
			LoadPercentage: c.LoadPercentage,
			WaitingInQueue: c.WaitingInQueue,
			Reasons:        []string{"high_concurrency"},
		}
		for _, m := range metrics {
			if m.UserID == userID {
				item.Current = m.Current
				item.Max = m.Max
				item.Min = m.Min
				item.Avg = m.Avg
				item.ZScore = zByUser[userID]
				item.Email = firstNonEmpty(item.Email, m.Email)
				item.Username = firstNonEmpty(item.Username, m.Username)
				break
			}
		}
		out = append(out, item)
	}

	// Also annotate already-included users with concurrency reasons.
	for i := range out {
		c := concurrency[out[i].UserID]
		if c == nil {
			continue
		}
		out[i].CurrentInUse = c.CurrentInUse
		out[i].MaxCapacity = c.MaxCapacity
		out[i].LoadPercentage = c.LoadPercentage
		out[i].WaitingInQueue = c.WaitingInQueue
		if c.LoadPercentage >= 90 || c.WaitingInQueue > 0 {
			if !containsString(out[i].Reasons, "high_concurrency") {
				out[i].Reasons = append(out[i].Reasons, "high_concurrency")
			}
		}
	}
	return out
}

// applyUserTrafficAnomalySticky increments per-user consecutive hit counts (~once
// per cache TTL), clears absentees, and returns all candidates with HitCount /
// Confirmed populated. Confirmed is advisory (hit_count >= confirm threshold);
// unconfirmed candidates are still returned so the UI can show emerging anomalies.
func applyUserTrafficAnomalySticky(candidates []UserTrafficAnomalyItem) []UserTrafficAnomalyItem {
	userTrafficAnomalyHitsMu.Lock()
	defer userTrafficAnomalyHitsMu.Unlock()

	present := map[int64]struct{}{}
	for _, c := range candidates {
		present[c.UserID] = struct{}{}
		userTrafficAnomalyHits[c.UserID]++
	}
	for id := range userTrafficAnomalyHits {
		if _, ok := present[id]; !ok {
			delete(userTrafficAnomalyHits, id)
		}
	}

	out := make([]UserTrafficAnomalyItem, 0, len(candidates))
	for _, c := range candidates {
		hits := userTrafficAnomalyHits[c.UserID]
		c.HitCount = hits
		c.Confirmed = hits >= userTrafficAnomalyConfirmHits
		out = append(out, c)
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func cloneUserTrafficAnomalyResponse(in *UserTrafficAnomalyResponse) *UserTrafficAnomalyResponse {
	if in == nil {
		return nil
	}
	out := *in
	if in.Items != nil {
		out.Items = make([]UserTrafficAnomalyItem, len(in.Items))
		copy(out.Items, in.Items)
		for i := range out.Items {
			if in.Items[i].Reasons != nil {
				out.Items[i].Reasons = append([]string(nil), in.Items[i].Reasons...)
			}
		}
	}
	return &out
}
