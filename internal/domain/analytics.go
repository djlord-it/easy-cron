package domain

// AnalyticsConfig defines per-job analytics settings.
// Analytics is strictly opt-in: if Enabled is false, no Redis writes occur.
type AnalyticsConfig struct {
	Enabled          bool
	RetentionSeconds int // TTL for analytics keys; uses DefaultRetentionSeconds if zero
}

// DefaultRetentionSeconds is 24 hours (86400 seconds).
const DefaultRetentionSeconds = 86400
