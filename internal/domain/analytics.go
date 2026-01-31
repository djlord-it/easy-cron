package domain

import "time"

type AnalyticsType string

const (
	AnalyticsTypeCount AnalyticsType = "count"
	AnalyticsTypeRate  AnalyticsType = "rate"
)

type AnalyticsConfig struct {
	Enabled   bool
	Type      AnalyticsType
	Window    time.Duration // 1m, 5m, 1h
	Retention time.Duration // TTL, must be >= Window
}
