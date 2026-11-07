package service

import "time"

// UserTrafficMinuteBucket is one user × minute aggregation from usage_logs.
type UserTrafficMinuteBucket struct {
	UserID    int64
	Email     string
	Username  string
	Bucket    time.Time
	Requests  int64
	Tokens    int64
	ActualCost float64
}

// UserTrafficAnomalySummary is population-level stats for the observation window.
type UserTrafficAnomalySummary struct {
	Window       string    `json:"window"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	ActiveUsers  int       `json:"active_users"`
	Max          float64   `json:"max"`
	Min          float64   `json:"min"`
	Avg          float64   `json:"avg"`
	Median       float64   `json:"median"`
	MAD          float64   `json:"mad"`
}

// UserTrafficAnomalyItem is one abnormal-user candidate (sticky hit_count attached).
type UserTrafficAnomalyItem struct {
	UserID         int64    `json:"user_id"`
	Email          string   `json:"email"`
	Username       string   `json:"username"`
	Current        float64  `json:"current"`
	Max            float64  `json:"max"`
	Min            float64  `json:"min"`
	Avg            float64  `json:"avg"`
	ZScore         float64  `json:"z_score"`
	CurrentInUse   int64    `json:"current_in_use"`
	MaxCapacity    int64    `json:"max_capacity"`
	LoadPercentage float64  `json:"load_percentage"`
	WaitingInQueue int64    `json:"waiting_in_queue"`
	Reasons        []string `json:"reasons"`
	HitCount       int      `json:"hit_count"`
	Confirmed      bool     `json:"confirmed"`
}

// UserTrafficAnomalyResponse is the admin traffic-monitor payload.
type UserTrafficAnomalyResponse struct {
	Enabled   bool                       `json:"enabled"`
	Timestamp time.Time                  `json:"timestamp"`
	Summary   UserTrafficAnomalySummary  `json:"summary"`
	Items     []UserTrafficAnomalyItem   `json:"items"`
}
