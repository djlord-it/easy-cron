package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/djlord-it/easy-cron/internal/domain"
)

var ErrDuplicateExecution = errors.New("execution already exists")

type Store interface {
	GetEnabledJobs(ctx context.Context) ([]JobWithSchedule, error)
	InsertExecution(ctx context.Context, exec domain.Execution) error
}

type CronParser interface {
	Parse(expression string, timezone string) (CronSchedule, error)
}

type CronSchedule interface {
	Next(after time.Time) time.Time
}

type EventEmitter interface {
	Emit(ctx context.Context, event domain.TriggerEvent) error
}

type JobWithSchedule struct {
	Job      domain.Job
	Schedule domain.Schedule
}

type Config struct {
	TickInterval time.Duration
}

type Scheduler struct {
	config   Config
	store    Store
	parser   CronParser
	emitter  EventEmitter
	clock    func() time.Time
	lastTick time.Time
}

func New(config Config, store Store, parser CronParser, emitter EventEmitter) *Scheduler {
	return &Scheduler{
		config:  config,
		store:   store,
		parser:  parser,
		emitter: emitter,
		clock:   time.Now,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.TickInterval)
	defer ticker.Stop()

	log.Printf("scheduler: started, tick=%s", s.config.TickInterval)
	s.lastTick = s.clock().UTC()

	for {
		select {
		case <-ctx.Done():
			log.Println("scheduler: stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := s.processTick(ctx); err != nil {
				log.Printf("scheduler: tick error: %v", err)
			}
		}
	}
}

func (s *Scheduler) processTick(ctx context.Context) error {
	now := s.clock().UTC()

	jobs, err := s.store.GetEnabledJobs(ctx)
	if err != nil {
		return fmt.Errorf("get jobs: %w", err)
	}

	for _, jws := range jobs {
		if err := s.processJob(ctx, jws, s.lastTick, now); err != nil {
			log.Printf("scheduler: job %s error: %v", jws.Job.ID, err)
		}
	}

	s.lastTick = now
	return nil
}

func (s *Scheduler) processJob(ctx context.Context, jws JobWithSchedule, lastTick, now time.Time) error {
	job := jws.Job
	schedule := jws.Schedule

	tz := schedule.Timezone
	if tz == "" {
		tz = "UTC"
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("load tz %s: %w", tz, err)
	}

	lastTickInTZ := lastTick.In(loc)
	nowInTZ := now.In(loc)

	cronSched, err := s.parser.Parse(schedule.CronExpression, tz)
	if err != nil {
		return fmt.Errorf("parse cron: %w", err)
	}

	// Loop through all due times since last tick
	const maxIterations = 1000
	t := cronSched.Next(lastTickInTZ)

	for i := 0; i < maxIterations && !t.After(nowInTZ); i++ {
		scheduledAtUTC := t.UTC().Truncate(time.Minute)

		// Check bounds
		if schedule.StartAt != nil && scheduledAtUTC.Before(*schedule.StartAt) {
			t = cronSched.Next(t)
			continue
		}
		if schedule.EndAt != nil && scheduledAtUTC.After(*schedule.EndAt) {
			t = cronSched.Next(t)
			continue
		}

		if err := s.emitExecution(ctx, job, scheduledAtUTC, now); err != nil {
			log.Printf("scheduler: job %s at %s error: %v", job.ID, scheduledAtUTC.Format(time.RFC3339), err)
		}

		t = cronSched.Next(t)
	}

	return nil
}

func (s *Scheduler) emitExecution(ctx context.Context, job domain.Job, scheduledAt, now time.Time) error {
	idempotencyKey := generateIdempotencyKey(job.ID, scheduledAt)
	executionID := uuid.New()

	execution := domain.Execution{
		ID:          executionID,
		JobID:       job.ID,
		ProjectID:   job.ProjectID,
		ScheduledAt: scheduledAt,
		FiredAt:     now,
		Status:      domain.ExecutionStatusEmitted,
		CreatedAt:   now,
	}

	if err := s.store.InsertExecution(ctx, execution); err != nil {
		if errors.Is(err, ErrDuplicateExecution) {
			return nil // already emitted
		}
		return fmt.Errorf("insert execution: %w", err)
	}

	event := domain.TriggerEvent{
		ExecutionID:    executionID,
		JobID:          job.ID,
		ProjectID:      job.ProjectID,
		ScheduledAt:    scheduledAt,
		FiredAt:        now,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
	}

	if err := s.emitter.Emit(ctx, event); err != nil {
		return fmt.Errorf("emit: %w", err)
	}

	log.Printf("scheduler: emitted job=%s scheduled_at=%s", job.ID, scheduledAt.Format(time.RFC3339))
	return nil
}

func generateIdempotencyKey(jobID uuid.UUID, scheduledAt time.Time) string {
	data := fmt.Sprintf("%s:%d", jobID.String(), scheduledAt.Unix())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
