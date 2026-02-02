package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/djlord-it/easy-cron/internal/domain"
)

var defaultBackoff = []time.Duration{
	0,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

const maxAttempts = 4

// ErrStatusTransitionDenied is returned when a status update would regress
// from a terminal state (delivered/failed).
var ErrStatusTransitionDenied = errors.New("status transition denied: execution already in terminal state")

type Store interface {
	GetJobByID(ctx context.Context, jobID uuid.UUID) (domain.Job, error)
	InsertDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) error
	// UpdateExecutionStatus sets the execution status. Implementations MUST
	// reject transitions from terminal states (delivered/failed) and return
	// ErrStatusTransitionDenied. This ensures idempotency on replay.
	UpdateExecutionStatus(ctx context.Context, executionID uuid.UUID, status domain.ExecutionStatus) error
}

type WebhookSender interface {
	Send(ctx context.Context, req WebhookRequest) WebhookResult
}

type AnalyticsSink interface {
	Record(ctx context.Context, event domain.TriggerEvent, config domain.AnalyticsConfig)
}

// MetricsSink defines the interface for recording dispatcher metrics.
// All methods must be non-blocking and fire-and-forget.
type MetricsSink interface {
	DeliveryAttemptCompleted(attempt int, statusClass string, duration time.Duration)
	DeliveryOutcome(outcome string)
	RetryAttempt(retryable bool)
	EventsInFlightIncr()
	EventsInFlightDecr()
}

type WebhookRequest struct {
	URL       string
	Secret    string
	Timeout   time.Duration
	Payload   WebhookPayload
	AttemptID string
}

type WebhookPayload struct {
	JobID       string `json:"job_id"`
	ExecutionID string `json:"execution_id"`
	ScheduledAt string `json:"scheduled_at"`
	FiredAt     string `json:"fired_at"`
}

type WebhookResult struct {
	StatusCode int
	Error      error
	Duration   time.Duration
}

func (r WebhookResult) IsSuccess() bool {
	return r.Error == nil && r.StatusCode >= 200 && r.StatusCode < 300
}

func (r WebhookResult) IsRetryable() bool {
	if r.Error != nil {
		return true
	}
	if r.StatusCode == 429 {
		return true
	}
	return r.StatusCode >= 500
}

type Dispatcher struct {
	store     Store
	sender    WebhookSender
	analytics AnalyticsSink // optional, nil = disabled
	metrics   MetricsSink   // optional, nil = disabled
	backoff   []time.Duration
}

func New(store Store, sender WebhookSender) *Dispatcher {
	return &Dispatcher{
		store:   store,
		sender:  sender,
		backoff: defaultBackoff,
	}
}

func (d *Dispatcher) WithAnalytics(sink AnalyticsSink) *Dispatcher {
	d.analytics = sink
	return d
}

// WithMetrics attaches a metrics sink to the dispatcher.
func (d *Dispatcher) WithMetrics(sink MetricsSink) *Dispatcher {
	d.metrics = sink
	return d
}

// Run processes events from the channel until context is cancelled.
// After cancellation, it drains remaining buffered events with a timeout.
func (d *Dispatcher) Run(ctx context.Context, ch <-chan domain.TriggerEvent) {
	for {
		select {
		case <-ctx.Done():
			d.drain(ch)
			return
		case event := <-ch:
			if err := d.Dispatch(ctx, event); err != nil {
				log.Printf("dispatcher: error: %v", err)
			}
		}
	}
}

// DrainTimeout is the maximum time to wait for buffered events during shutdown.
const DrainTimeout = 30 * time.Second

// drain processes remaining events in the channel buffer after shutdown signal.
// Uses a background context since the main context is already cancelled.
func (d *Dispatcher) drain(ch <-chan domain.TriggerEvent) {
	drainCtx, cancel := context.WithTimeout(context.Background(), DrainTimeout)
	defer cancel()

	count := 0
	for {
		select {
		case <-drainCtx.Done():
			if count > 0 {
				log.Printf("dispatcher: drain timeout, processed %d events", count)
			}
			return
		case event, ok := <-ch:
			if !ok {
				// Channel closed
				log.Printf("dispatcher: drain complete, processed %d events", count)
				return
			}
			if err := d.Dispatch(drainCtx, event); err != nil {
				log.Printf("dispatcher: drain error: %v", err)
			}
			count++
		default:
			// No more buffered events
			if count > 0 {
				log.Printf("dispatcher: drain complete, processed %d events", count)
			}
			return
		}
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, event domain.TriggerEvent) error {
	// Track in-flight events
	if d.metrics != nil {
		d.metrics.EventsInFlightIncr()
		defer d.metrics.EventsInFlightDecr()
	}

	job, err := d.store.GetJobByID(ctx, event.JobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	// Write analytics immediately on every TriggerEvent, independent of delivery outcome.
	// This counts executions, not successful deliveries.
	d.writeAnalytics(ctx, event, job)

	if job.Delivery.WebhookURL == "" {
		log.Printf("dispatcher: job=%s has no webhook URL configured", event.JobID)
		return fmt.Errorf("job %s: no webhook URL", event.JobID)
	}

	payload := WebhookPayload{
		JobID:       event.JobID.String(),
		ExecutionID: event.ExecutionID.String(),
		ScheduledAt: event.ScheduledAt.UTC().Format(time.RFC3339),
		FiredAt:     event.FiredAt.UTC().Format(time.RFC3339),
	}

	req := WebhookRequest{
		URL:     job.Delivery.WebhookURL,
		Secret:  job.Delivery.Secret,
		Timeout: job.Delivery.Timeout,
		Payload: payload,
	}

	var lastResult WebhookResult

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			// Record retry attempt metric
			if d.metrics != nil {
				d.metrics.RetryAttempt(lastResult.IsRetryable())
			}

			idx := attempt - 1
			if idx >= len(d.backoff) {
				idx = len(d.backoff) - 1
			}
			backoff := d.backoff[idx]

			log.Printf("dispatcher: job=%s attempt=%d backoff=%s", event.JobID, attempt, backoff)

			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			case <-timer.C:
			}
		}

		attemptID := uuid.New()
		req.AttemptID = attemptID.String()

		startedAt := time.Now().UTC()
		result := d.sender.Send(ctx, req)
		finishedAt := time.Now().UTC()
		lastResult = result

		// Record delivery attempt metrics
		if d.metrics != nil {
			statusClass := classifyStatusForMetrics(result.StatusCode, result.Error)
			d.metrics.DeliveryAttemptCompleted(attempt, statusClass, result.Duration)
		}

		attemptRecord := domain.DeliveryAttempt{
			ID:          attemptID,
			ExecutionID: event.ExecutionID,
			Attempt:     attempt,
			StatusCode:  result.StatusCode,
			StartedAt:   startedAt,
			FinishedAt:  finishedAt,
		}
		if result.Error != nil {
			attemptRecord.Error = result.Error.Error()
		}

		if err := d.store.InsertDeliveryAttempt(ctx, attemptRecord); err != nil {
			log.Printf("dispatcher: failed to record attempt: %v", err)
		}

		if result.IsSuccess() {
			log.Printf("dispatcher: job=%s delivered attempt=%d", event.JobID, attempt)
			if d.metrics != nil {
				d.metrics.DeliveryOutcome("success")
			}
			if err := d.store.UpdateExecutionStatus(ctx, event.ExecutionID, domain.ExecutionStatusDelivered); err != nil {
				if errors.Is(err, ErrStatusTransitionDenied) {
					// Execution already in terminal state (likely reprocessing). Safe to ignore.
					log.Printf("dispatcher: job=%s execution=%s already terminal, skipping status update", event.JobID, event.ExecutionID)
					return nil
				}
				return err
			}
			return nil
		}

		if !result.IsRetryable() {
			log.Printf("dispatcher: job=%s non-retryable status=%d", event.JobID, result.StatusCode)
			break
		}

		log.Printf("dispatcher: job=%s attempt=%d failed status=%d err=%v", event.JobID, attempt, result.StatusCode, result.Error)
	}

	log.Printf("dispatcher: job=%s failed status=%d err=%v", event.JobID, lastResult.StatusCode, lastResult.Error)
	if d.metrics != nil {
		d.metrics.DeliveryOutcome("failed")
	}
	if err := d.store.UpdateExecutionStatus(ctx, event.ExecutionID, domain.ExecutionStatusFailed); err != nil {
		if errors.Is(err, ErrStatusTransitionDenied) {
			// Execution already in terminal state (likely reprocessing). Safe to ignore.
			log.Printf("dispatcher: job=%s execution=%s already terminal, skipping status update", event.JobID, event.ExecutionID)
			return nil
		}
		return err
	}
	return nil
}

// writeAnalytics records execution metrics as a best-effort side-effect.
// The sink handles errors internally; analytics never affects dispatch correctness.
func (d *Dispatcher) writeAnalytics(ctx context.Context, event domain.TriggerEvent, job domain.Job) {
	if d.analytics == nil {
		if job.Analytics.Enabled {
			log.Printf("dispatcher: job=%s analytics enabled but no sink configured (metrics not recorded)", event.JobID)
		}
		return
	}
	if !job.Analytics.Enabled {
		return
	}
	d.analytics.Record(ctx, event, job.Analytics)
}

// classifyStatusForMetrics maps an HTTP status code and error to a metrics status class.
// Uses bounded cardinality: 2xx, 4xx, 5xx, timeout, connection_error, other_error.
func classifyStatusForMetrics(statusCode int, err error) string {
	if err != nil {
		errStr := err.Error()
		// Check for timeout errors
		if containsInsensitive(errStr, "timeout") || containsInsensitive(errStr, "deadline exceeded") {
			return "timeout"
		}
		// Check for connection errors
		if containsInsensitive(errStr, "connection refused") ||
			containsInsensitive(errStr, "no such host") ||
			containsInsensitive(errStr, "network is unreachable") ||
			containsInsensitive(errStr, "dial") {
			return "connection_error"
		}
		return "other_error"
	}

	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500:
		return "5xx"
	default:
		return "other_error"
	}
}

// containsInsensitive checks if substr is in s (case-insensitive).
func containsInsensitive(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			if c1 != c2 {
				// Convert to lowercase
				if c1 >= 'A' && c1 <= 'Z' {
					c1 += 32
				}
				if c2 >= 'A' && c2 <= 'Z' {
					c2 += 32
				}
				if c1 != c2 {
					match = false
					break
				}
			}
		}
		if match {
			return true
		}
	}
	return false
}
