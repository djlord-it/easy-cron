package metrics

import "time"

// Sink defines the interface for recording metrics.
// All methods are fire-and-forget: implementations MUST NOT block or propagate errors.
// If the metrics backend is unavailable, implementations log warnings and continue.
type Sink interface {
	// Scheduler metrics
	TickStarted()
	TickCompleted(duration time.Duration, jobsTriggered int, err error)
	TickDrift(drift time.Duration)

	// Dispatcher metrics
	DeliveryAttemptCompleted(attempt int, statusClass string, duration time.Duration)
	DeliveryOutcome(outcome string)
	RetryAttempt(retryable bool)
	EventsInFlightIncr()
	EventsInFlightDecr()

	// EventBus metrics
	BufferSizeUpdate(size int)
	BufferCapacitySet(capacity int)
	BufferSaturationUpdate(saturation float64)
	EmitError()

	// Observability metrics (C4)
	OrphanedExecutionsUpdate(count int)
	ExecutionLatencyObserve(latencySeconds float64)
}

// Outcome constants for DeliveryOutcome metric.
const (
	OutcomeSuccess   = "success"
	OutcomeFailed    = "failed"
	OutcomeAbandoned = "abandoned"
)

// StatusClass constants for DeliveryAttemptCompleted metric.
const (
	StatusClass2xx             = "2xx"
	StatusClass4xx             = "4xx"
	StatusClass5xx             = "5xx"
	StatusClassTimeout         = "timeout"
	StatusClassConnectionError = "connection_error"
	StatusClassOtherError      = "other_error"
)

// ClassifyStatus maps a status code and error to a status class.
func ClassifyStatus(statusCode int, err error) string {
	if err != nil {
		errStr := err.Error()
		// Check for timeout errors
		if contains(errStr, "timeout") || contains(errStr, "deadline exceeded") {
			return StatusClassTimeout
		}
		// Check for connection errors
		if contains(errStr, "connection refused") || contains(errStr, "no such host") ||
			contains(errStr, "network is unreachable") || contains(errStr, "dial") {
			return StatusClassConnectionError
		}
		return StatusClassOtherError
	}

	switch {
	case statusCode >= 200 && statusCode < 300:
		return StatusClass2xx
	case statusCode >= 400 && statusCode < 500:
		return StatusClass4xx
	case statusCode >= 500:
		return StatusClass5xx
	default:
		return StatusClassOtherError
	}
}

// contains is a simple case-insensitive substring check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchInsensitive(s, substr)
}

func searchInsensitive(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFoldAt(s, i, substr) {
			return true
		}
	}
	return false
}

func equalFoldAt(s string, offset int, substr string) bool {
	for j := 0; j < len(substr); j++ {
		c1 := s[offset+j]
		c2 := substr[j]
		if c1 != c2 && toLower(c1) != toLower(c2) {
			return false
		}
	}
	return true
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}
