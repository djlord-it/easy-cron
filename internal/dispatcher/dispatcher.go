package dispatcher

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"easycron/internal/domain"
)

var defaultBackoff = []time.Duration{
	0,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

const maxAttempts = 4

type Store interface {
	GetJobByID(ctx context.Context, jobID uuid.UUID) (domain.Job, error)
	InsertDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) error
	UpdateExecutionStatus(ctx context.Context, executionID uuid.UUID, status domain.ExecutionStatus) error
}

type WebhookSender interface {
	Send(ctx context.Context, req WebhookRequest) WebhookResult
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
	store   Store
	sender  WebhookSender
	backoff []time.Duration
}

func New(store Store, sender WebhookSender) *Dispatcher {
	return &Dispatcher{
		store:   store,
		sender:  sender,
		backoff: defaultBackoff,
	}
}

func (d *Dispatcher) Run(ctx context.Context, ch <-chan domain.TriggerEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ch:
			if err := d.Dispatch(ctx, event); err != nil {
				log.Printf("dispatcher: error: %v", err)
			}
		}
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, event domain.TriggerEvent) error {
	job, err := d.store.GetJobByID(ctx, event.JobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	if job.Delivery.WebhookURL == "" {
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
			return d.store.UpdateExecutionStatus(ctx, event.ExecutionID, domain.ExecutionStatusDelivered)
		}

		if !result.IsRetryable() {
			log.Printf("dispatcher: job=%s non-retryable status=%d", event.JobID, result.StatusCode)
			break
		}

		log.Printf("dispatcher: job=%s attempt=%d failed status=%d err=%v", event.JobID, attempt, result.StatusCode, result.Error)
	}

	log.Printf("dispatcher: job=%s failed status=%d err=%v", event.JobID, lastResult.StatusCode, lastResult.Error)
	return d.store.UpdateExecutionStatus(ctx, event.ExecutionID, domain.ExecutionStatusFailed)
}
