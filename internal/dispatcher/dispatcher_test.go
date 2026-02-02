package dispatcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/djlord-it/easy-cron/internal/domain"
)

// mockStore tracks execution status transitions and enforces terminal state guards.
type mockStore struct {
	mu               sync.Mutex
	jobs             map[uuid.UUID]domain.Job
	executionStatus  map[uuid.UUID]domain.ExecutionStatus
	deliveryAttempts []domain.DeliveryAttempt
	statusUpdates    []statusUpdate
}

type statusUpdate struct {
	ExecutionID uuid.UUID
	Status      domain.ExecutionStatus
	Denied      bool
}

func newMockStore() *mockStore {
	return &mockStore{
		jobs:            make(map[uuid.UUID]domain.Job),
		executionStatus: make(map[uuid.UUID]domain.ExecutionStatus),
	}
}

func (s *mockStore) GetJobByID(ctx context.Context, jobID uuid.UUID) (domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return domain.Job{}, errors.New("job not found")
	}
	return job, nil
}

func (s *mockStore) InsertDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveryAttempts = append(s.deliveryAttempts, attempt)
	return nil
}

func (s *mockStore) UpdateExecutionStatus(ctx context.Context, executionID uuid.UUID, status domain.ExecutionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentStatus := s.executionStatus[executionID]

	// Enforce terminal state guard
	if currentStatus == domain.ExecutionStatusDelivered || currentStatus == domain.ExecutionStatusFailed {
		s.statusUpdates = append(s.statusUpdates, statusUpdate{
			ExecutionID: executionID,
			Status:      status,
			Denied:      true,
		})
		return ErrStatusTransitionDenied
	}

	s.executionStatus[executionID] = status
	s.statusUpdates = append(s.statusUpdates, statusUpdate{
		ExecutionID: executionID,
		Status:      status,
		Denied:      false,
	})
	return nil
}

func (s *mockStore) setExecutionStatus(id uuid.UUID, status domain.ExecutionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executionStatus[id] = status
}

func (s *mockStore) getExecutionStatus(id uuid.UUID) domain.ExecutionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executionStatus[id]
}

func (s *mockStore) getStatusUpdates() []statusUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]statusUpdate, len(s.statusUpdates))
	copy(result, s.statusUpdates)
	return result
}

func (s *mockStore) getAttemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.deliveryAttempts)
}

func (s *mockStore) addJob(job domain.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

// mockSender simulates webhook delivery with configurable results.
type mockSender struct {
	mu      sync.Mutex
	results []WebhookResult
	index   int
	calls   int
}

func (s *mockSender) Send(ctx context.Context, req WebhookRequest) WebhookResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.index < len(s.results) {
		result := s.results[s.index]
		s.index++
		return result
	}
	// Default: success
	return WebhookResult{StatusCode: 200, Duration: 10 * time.Millisecond}
}

func (s *mockSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestDispatcher_TerminalState_DeliveredCannotRegress verifies that once an
// execution is marked as delivered, it cannot be changed to any other status.
func TestDispatcher_TerminalState_DeliveredCannotRegress(t *testing.T) {
	store := newMockStore()
	sender := &mockSender{results: []WebhookResult{{StatusCode: 200}}}

	jobID := uuid.New()
	executionID := uuid.New()

	store.addJob(domain.Job{
		ID:        jobID,
		ProjectID: uuid.New(),
		Name:      "test-job",
		Enabled:   true,
		Delivery: domain.DeliveryConfig{
			Type:       "webhook",
			WebhookURL: "http://example.com/webhook",
			Timeout:    30 * time.Second,
		},
	})

	// Pre-set execution as delivered (simulating already processed)
	store.setExecutionStatus(executionID, domain.ExecutionStatusDelivered)

	disp := New(store, sender)

	event := domain.TriggerEvent{
		ExecutionID: executionID,
		JobID:       jobID,
		ProjectID:   uuid.New(),
		ScheduledAt: time.Now().UTC(),
		FiredAt:     time.Now().UTC(),
	}

	ctx := context.Background()
	err := disp.Dispatch(ctx, event)

	// Should succeed (idempotent handling)
	if err != nil {
		t.Fatalf("dispatch should succeed on replay: %v", err)
	}

	// Status should still be delivered
	if store.getExecutionStatus(executionID) != domain.ExecutionStatusDelivered {
		t.Error("execution status should remain delivered")
	}

	// Should have recorded the denied transition
	updates := store.getStatusUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 status update attempt, got %d", len(updates))
	}
	if !updates[0].Denied {
		t.Error("status update should have been denied")
	}
}

// TestDispatcher_TerminalState_FailedCannotRegress verifies that once an
// execution is marked as failed, it cannot be changed to any other status.
func TestDispatcher_TerminalState_FailedCannotRegress(t *testing.T) {
	store := newMockStore()
	sender := &mockSender{results: []WebhookResult{{StatusCode: 200}}}

	jobID := uuid.New()
	executionID := uuid.New()

	store.addJob(domain.Job{
		ID:        jobID,
		ProjectID: uuid.New(),
		Name:      "test-job",
		Enabled:   true,
		Delivery: domain.DeliveryConfig{
			Type:       "webhook",
			WebhookURL: "http://example.com/webhook",
			Timeout:    30 * time.Second,
		},
	})

	// Pre-set execution as failed
	store.setExecutionStatus(executionID, domain.ExecutionStatusFailed)

	disp := New(store, sender)

	event := domain.TriggerEvent{
		ExecutionID: executionID,
		JobID:       jobID,
		ProjectID:   uuid.New(),
		ScheduledAt: time.Now().UTC(),
		FiredAt:     time.Now().UTC(),
	}

	ctx := context.Background()
	err := disp.Dispatch(ctx, event)

	// Should succeed (idempotent handling)
	if err != nil {
		t.Fatalf("dispatch should succeed on replay: %v", err)
	}

	// Status should still be failed
	if store.getExecutionStatus(executionID) != domain.ExecutionStatusFailed {
		t.Error("execution status should remain failed")
	}
}

// TestDispatcher_RetryBounded verifies that retry attempts are bounded
// to exactly maxAttempts (4).
func TestDispatcher_RetryBounded(t *testing.T) {
	store := newMockStore()

	// All attempts fail with retryable error
	sender := &mockSender{results: []WebhookResult{
		{StatusCode: 500}, // Attempt 1: retryable
		{StatusCode: 500}, // Attempt 2: retryable
		{StatusCode: 500}, // Attempt 3: retryable
		{StatusCode: 500}, // Attempt 4: retryable
		{StatusCode: 500}, // Should never reach this
	}}

	jobID := uuid.New()
	executionID := uuid.New()

	store.addJob(domain.Job{
		ID:        jobID,
		ProjectID: uuid.New(),
		Name:      "test-job",
		Enabled:   true,
		Delivery: domain.DeliveryConfig{
			Type:       "webhook",
			WebhookURL: "http://example.com/webhook",
			Timeout:    30 * time.Second,
		},
	})

	// Use zero backoff for fast test
	disp := New(store, sender)
	disp.backoff = []time.Duration{0, 0, 0, 0}

	event := domain.TriggerEvent{
		ExecutionID: executionID,
		JobID:       jobID,
		ProjectID:   uuid.New(),
		ScheduledAt: time.Now().UTC(),
		FiredAt:     time.Now().UTC(),
	}

	ctx := context.Background()
	_ = disp.Dispatch(ctx, event)

	// Should have exactly 4 webhook calls (maxAttempts)
	if sender.callCount() != 4 {
		t.Errorf("expected exactly 4 webhook calls, got %d", sender.callCount())
	}

	// Should have exactly 4 delivery attempts recorded
	if store.getAttemptCount() != 4 {
		t.Errorf("expected exactly 4 delivery attempts, got %d", store.getAttemptCount())
	}

	// Execution should be marked failed
	if store.getExecutionStatus(executionID) != domain.ExecutionStatusFailed {
		t.Error("execution should be marked failed after max retries")
	}
}

// TestDispatcher_NonRetryableStopsImmediately verifies that non-retryable
// errors (4xx except 429) stop retry immediately.
func TestDispatcher_NonRetryableStopsImmediately(t *testing.T) {
	store := newMockStore()

	// First attempt returns non-retryable 404
	sender := &mockSender{results: []WebhookResult{
		{StatusCode: 404}, // Non-retryable
		{StatusCode: 200}, // Should never reach
	}}

	jobID := uuid.New()
	executionID := uuid.New()

	store.addJob(domain.Job{
		ID:        jobID,
		ProjectID: uuid.New(),
		Name:      "test-job",
		Enabled:   true,
		Delivery: domain.DeliveryConfig{
			Type:       "webhook",
			WebhookURL: "http://example.com/webhook",
			Timeout:    30 * time.Second,
		},
	})

	disp := New(store, sender)
	disp.backoff = []time.Duration{0, 0, 0, 0}

	event := domain.TriggerEvent{
		ExecutionID: executionID,
		JobID:       jobID,
		ProjectID:   uuid.New(),
		ScheduledAt: time.Now().UTC(),
		FiredAt:     time.Now().UTC(),
	}

	ctx := context.Background()
	_ = disp.Dispatch(ctx, event)

	// Should have only 1 webhook call (non-retryable)
	if sender.callCount() != 1 {
		t.Errorf("expected 1 webhook call for non-retryable error, got %d", sender.callCount())
	}

	// Execution should be marked failed
	if store.getExecutionStatus(executionID) != domain.ExecutionStatusFailed {
		t.Error("execution should be marked failed for non-retryable error")
	}
}

// TestDispatcher_429IsRetryable verifies that 429 (rate limit) is retryable.
func TestDispatcher_429IsRetryable(t *testing.T) {
	store := newMockStore()

	sender := &mockSender{results: []WebhookResult{
		{StatusCode: 429}, // Retryable
		{StatusCode: 200}, // Success on retry
	}}

	jobID := uuid.New()
	executionID := uuid.New()

	store.addJob(domain.Job{
		ID:        jobID,
		ProjectID: uuid.New(),
		Name:      "test-job",
		Enabled:   true,
		Delivery: domain.DeliveryConfig{
			Type:       "webhook",
			WebhookURL: "http://example.com/webhook",
			Timeout:    30 * time.Second,
		},
	})

	disp := New(store, sender)
	disp.backoff = []time.Duration{0, 0, 0, 0}

	event := domain.TriggerEvent{
		ExecutionID: executionID,
		JobID:       jobID,
		ProjectID:   uuid.New(),
		ScheduledAt: time.Now().UTC(),
		FiredAt:     time.Now().UTC(),
	}

	ctx := context.Background()
	_ = disp.Dispatch(ctx, event)

	// Should have 2 webhook calls (retry after 429)
	if sender.callCount() != 2 {
		t.Errorf("expected 2 webhook calls (429 is retryable), got %d", sender.callCount())
	}

	// Execution should be marked delivered
	if store.getExecutionStatus(executionID) != domain.ExecutionStatusDelivered {
		t.Error("execution should be marked delivered after successful retry")
	}
}

// TestDispatcher_MaxAttemptsConstant verifies the maxAttempts constant is exactly 4.
func TestDispatcher_MaxAttemptsConstant(t *testing.T) {
	if maxAttempts != 4 {
		t.Errorf("maxAttempts must be 4, got %d", maxAttempts)
	}
}
