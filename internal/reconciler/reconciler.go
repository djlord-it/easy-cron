// Package reconciler detects and re-emits orphaned executions.
//
// An execution is orphaned when it has status='emitted' but was never
// delivered to the dispatcher (e.g., due to buffer overflow or crash).
//
// The reconciler periodically scans for orphaned executions and re-emits
// them to the event bus. Idempotency is guaranteed by the dispatcher's
// terminal state guards - if an execution was already processed, the
// re-emit is safely ignored.
package reconciler

import (
	"context"
	"log"
	"time"

	"github.com/djlord-it/easy-cron/internal/domain"
)

// Store defines the interface for fetching orphaned executions.
type Store interface {
	GetOrphanedExecutions(ctx context.Context, olderThan time.Time, maxResults int) ([]domain.Execution, error)
}

// EventEmitter defines the interface for emitting trigger events.
type EventEmitter interface {
	Emit(ctx context.Context, event domain.TriggerEvent) error
}

// Config holds reconciler configuration.
type Config struct {
	// Interval is how often the reconciler runs.
	// Default: 5 minutes.
	Interval time.Duration

	// Threshold is the age after which an emitted execution is considered orphaned.
	// Default: 10 minutes.
	Threshold time.Duration

	// BatchSize is the maximum number of orphans to process per cycle.
	// Default: 100.
	BatchSize int
}

// DefaultConfig returns the default reconciler configuration.
func DefaultConfig() Config {
	return Config{
		Interval:  5 * time.Minute,
		Threshold: 10 * time.Minute,
		BatchSize: 100,
	}
}

// Reconciler detects orphaned executions and re-emits them.
type Reconciler struct {
	config  Config
	store   Store
	emitter EventEmitter
	clock   func() time.Time
}

// New creates a new Reconciler.
func New(config Config, store Store, emitter EventEmitter) *Reconciler {
	return &Reconciler{
		config:  config,
		store:   store,
		emitter: emitter,
		clock:   time.Now,
	}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	log.Printf("reconciler: started (interval=%s, threshold=%s, batch=%d)",
		r.config.Interval, r.config.Threshold, r.config.BatchSize)

	// Run immediately on startup, then on ticker
	r.runCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("reconciler: stopped")
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

// runCycle executes one reconciliation cycle.
func (r *Reconciler) runCycle(ctx context.Context) {
	now := r.clock().UTC()
	threshold := now.Add(-r.config.Threshold)

	orphans, err := r.store.GetOrphanedExecutions(ctx, threshold, r.config.BatchSize)
	if err != nil {
		// DB error: log and abort cycle. Will retry next interval.
		log.Printf("reconciler: failed to fetch orphans: %v", err)
		return
	}

	if len(orphans) == 0 {
		// Nothing to do. Silent success.
		return
	}

	log.Printf("reconciler: found %d orphaned executions", len(orphans))

	emitted := 0
	failed := 0

	for _, exec := range orphans {
		// Check context before each emit to allow graceful shutdown
		if ctx.Err() != nil {
			log.Printf("reconciler: cycle interrupted, processed %d/%d orphans", emitted+failed, len(orphans))
			return
		}

		event := domain.TriggerEvent{
			ExecutionID: exec.ID,
			JobID:       exec.JobID,
			ProjectID:   exec.ProjectID,
			ScheduledAt: exec.ScheduledAt,
			FiredAt:     exec.FiredAt,
			CreatedAt:   now,
		}

		if err := r.emitter.Emit(ctx, event); err != nil {
			// Emit failed (buffer full, context cancelled).
			// Log and continue - will retry next cycle.
			log.Printf("reconciler: failed to re-emit execution=%s job=%s: %v",
				exec.ID, exec.JobID, err)
			failed++
			continue
		}

		log.Printf("reconciler: re-emitted execution=%s job=%s scheduled_at=%s (age=%s)",
			exec.ID, exec.JobID, exec.ScheduledAt.Format(time.RFC3339),
			now.Sub(exec.CreatedAt).Round(time.Second))
		emitted++
	}

	log.Printf("reconciler: cycle complete, re-emitted=%d, failed=%d", emitted, failed)
}
