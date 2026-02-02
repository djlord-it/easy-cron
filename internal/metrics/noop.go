package metrics

import "time"

// NoopSink is a no-op implementation of Sink.
// Used when metrics are disabled to avoid nil checks.
type NoopSink struct{}

// NewNoopSink returns a no-op metrics sink.
func NewNoopSink() *NoopSink {
	return &NoopSink{}
}

func (n *NoopSink) TickStarted()                                                              {}
func (n *NoopSink) TickCompleted(duration time.Duration, jobsTriggered int, err error)        {}
func (n *NoopSink) TickDrift(drift time.Duration)                                             {}
func (n *NoopSink) DeliveryAttemptCompleted(attempt int, statusClass string, d time.Duration) {}
func (n *NoopSink) DeliveryOutcome(outcome string)                                            {}
func (n *NoopSink) RetryAttempt(retryable bool)                                               {}
func (n *NoopSink) EventsInFlightIncr()                                                       {}
func (n *NoopSink) EventsInFlightDecr()                                                       {}
func (n *NoopSink) BufferSizeUpdate(size int)                                                 {}
func (n *NoopSink) EmitError()                                                                {}
