package scheduler

import (
	"context"
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

// MetricsSink defines the interface for recording scheduler metrics.
// All methods must be non-blocking and fire-and-forget.
type MetricsSink interface {
	TickStarted()
	TickCompleted(duration time.Duration, jobsTriggered int, err error)
	TickDrift(drift time.Duration)
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
	metrics  MetricsSink // optional, nil = disabled
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

// WithMetrics attaches a metrics sink to the scheduler.
func (s *Scheduler) WithMetrics(sink MetricsSink) *Scheduler {
	s.metrics = sink
	return s
}

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.TickInterval)
	defer ticker.Stop()

	log.Printf("scheduler: started, tick=%s", s.config.TickInterval)
	s.lastTick = s.clock().UTC()
	expectedNext := s.lastTick.Add(s.config.TickInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("scheduler: stopped")
			return ctx.Err()
		case tickTime := <-ticker.C:
			// Record tick drift before processing
			if s.metrics != nil {
				drift := tickTime.Sub(expectedNext)
				s.metrics.TickDrift(drift)
			}
			expectedNext = tickTime.Add(s.config.TickInterval)

			if err := s.processTick(ctx); err != nil {
				log.Printf("scheduler: tick error: %v", err)
			}
		}
	}
}

func (s *Scheduler) processTick(ctx context.Context) error {
	if s.metrics != nil {
		s.metrics.TickStarted()
	}

	start := s.clock()
	now := start.UTC()
	jobsTriggered := 0

	jobs, err := s.store.GetEnabledJobs(ctx)
	if err != nil {
		if s.metrics != nil {
			s.metrics.TickCompleted(s.clock().Sub(start), 0, err)
		}
		return fmt.Errorf("get jobs: %w", err)
	}

	for _, jws := range jobs {
		triggered, jobErr := s.processJob(ctx, jws, s.lastTick, now)
		jobsTriggered += triggered
		if jobErr != nil {
			log.Printf("scheduler: job=%s project=%s error: %v", jws.Job.ID, jws.Job.ProjectID, jobErr)
		}
	}

	s.lastTick = now

	if s.metrics != nil {
		s.metrics.TickCompleted(s.clock().Sub(start), jobsTriggered, nil)
	}

	return nil
}

func (s *Scheduler) processJob(ctx context.Context, jws JobWithSchedule, lastTick, now time.Time) (int, error) {
	job := jws.Job
	schedule := jws.Schedule

	tz := schedule.Timezone
	if tz == "" {
		tz = "UTC"
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return 0, fmt.Errorf("load tz %s: %w", tz, err)
	}

	lastTickInTZ := lastTick.In(loc)
	nowInTZ := now.In(loc)

	cronSched, err := s.parser.Parse(schedule.CronExpression, tz)
	if err != nil {
		return 0, fmt.Errorf("parse cron: %w", err)
	}

	// Loop through all due times since last tick
	const maxIterations = 1000
	t := cronSched.Next(lastTickInTZ)
	triggered := 0

	for i := 0; i < maxIterations && !t.After(nowInTZ); i++ {
		scheduledAtUTC := t.UTC().Truncate(time.Minute)

		if err := s.emitExecution(ctx, job, scheduledAtUTC, now); err != nil {
			log.Printf("scheduler: job=%s project=%s at %s error: %v", job.ID, job.ProjectID, scheduledAtUTC.Format(time.RFC3339), err)
		} else {
			triggered++
		}

		t = cronSched.Next(t)
	}

	return triggered, nil
}

func (s *Scheduler) emitExecution(ctx context.Context, job domain.Job, scheduledAt, now time.Time) error {
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
		ExecutionID: executionID,
		JobID:       job.ID,
		ProjectID:   job.ProjectID,
		ScheduledAt: scheduledAt,
		FiredAt:     now,
		CreatedAt:   now,
	}

	if err := s.emitter.Emit(ctx, event); err != nil {
		return fmt.Errorf("emit: %w", err)
	}

	log.Printf("scheduler: emitted job=%s project=%s scheduled_at=%s", job.ID, job.ProjectID, scheduledAt.Format(time.RFC3339))
	return nil
}
