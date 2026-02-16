package metrics

import (
	"testing"
	"time"
)

func TestNoopSink_AllMethods(t *testing.T) {
	// Verify that calling all methods on NoopSink does not panic.
	s := NewNoopSink()

	// Scheduler metrics
	s.TickStarted()
	s.TickCompleted(100*time.Millisecond, 5, nil)
	s.TickCompleted(100*time.Millisecond, 0, nil)
	s.TickDrift(10 * time.Millisecond)

	// Dispatcher metrics
	s.DeliveryAttemptCompleted(1, StatusClass2xx, 200*time.Millisecond)
	s.DeliveryOutcome(OutcomeSuccess)
	s.DeliveryOutcome(OutcomeFailed)
	s.DeliveryOutcome(OutcomeAbandoned)
	s.RetryAttempt(true)
	s.RetryAttempt(false)
	s.EventsInFlightIncr()
	s.EventsInFlightDecr()

	// EventBus metrics
	s.BufferSizeUpdate(10)
	s.BufferCapacitySet(100)
	s.BufferSaturationUpdate(0.1)
	s.EmitError()

	// Observability metrics
	s.OrphanedExecutionsUpdate(3)
	s.ExecutionLatencyObserve(1.5)
}

// Verify NoopSink implements Sink interface.
var _ Sink = (*NoopSink)(nil)
