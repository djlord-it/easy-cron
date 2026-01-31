package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/djlord-it/easy-cron/internal/analytics"
	"github.com/djlord-it/easy-cron/internal/dispatcher"
	"github.com/djlord-it/easy-cron/internal/domain"
	"github.com/djlord-it/easy-cron/internal/scheduler"
	"github.com/djlord-it/easy-cron/internal/transport/channel"
)

func main() {
	// TODO: replace with real implementations
	store := &stubStore{}
	cronParser := &stubCronParser{}

	// WARNING: Stub implementations are active. Jobs will not execute correctly.
	log.Println("worker: WARNING - using stub store and cron parser (replace with real implementations before production)")

	bus := channel.NewEventBus(100)
	webhookSender := dispatcher.NewHTTPWebhookSender()

	sched := scheduler.New(
		scheduler.Config{TickInterval: 30 * time.Second},
		store,
		cronParser,
		bus,
	)

	disp := dispatcher.New(store, webhookSender)

	// Wire analytics if Redis is configured
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr: redisAddr,
		})
		sink := analytics.NewRedisSink(redisClient)
		disp = disp.WithAnalytics(sink)
		log.Printf("worker: analytics enabled (redis=%s)", redisAddr)
	} else {
		log.Println("worker: WARNING - REDIS_ADDR not set; analytics will be disabled even if jobs have analytics enabled")
	}

	// Use separate contexts for scheduler and dispatcher to enable ordered shutdown.
	// Scheduler stops first (no new events), then dispatcher drains remaining events.
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	dispatcherCtx, cancelDispatcher := context.WithCancel(context.Background())

	var schedulerWg sync.WaitGroup
	var dispatcherWg sync.WaitGroup

	schedulerWg.Add(1)
	go func() {
		defer schedulerWg.Done()
		sched.Run(schedulerCtx)
	}()

	dispatcherWg.Add(1)
	go func() {
		defer dispatcherWg.Done()
		disp.Run(dispatcherCtx, bus.Channel())
	}()

	log.Println("worker: started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	received := <-sig

	log.Printf("worker: received signal %v, shutting down", received)

	// Phase 1: Stop scheduler (no new events emitted)
	log.Println("worker: stopping scheduler...")
	cancelScheduler()
	schedulerWg.Wait()
	log.Println("worker: scheduler stopped")

	// Phase 2: Stop dispatcher (will drain buffered events before returning)
	log.Println("worker: stopping dispatcher (draining events)...")
	cancelDispatcher()
	dispatcherWg.Wait()
	log.Println("worker: dispatcher stopped")

	log.Println("worker: stopped")
}

// Stubs for compilation. Replace with real store/parser implementations.

type stubStore struct{}

func (s *stubStore) GetEnabledJobs(ctx context.Context) ([]scheduler.JobWithSchedule, error) {
	return nil, nil
}

func (s *stubStore) InsertExecution(ctx context.Context, exec domain.Execution) error {
	return nil
}

func (s *stubStore) GetJobByID(ctx context.Context, jobID uuid.UUID) (domain.Job, error) {
	return domain.Job{}, nil
}

func (s *stubStore) InsertDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) error {
	return nil
}

func (s *stubStore) UpdateExecutionStatus(ctx context.Context, executionID uuid.UUID, status domain.ExecutionStatus) error {
	return nil
}

type stubCronParser struct{}

func (p *stubCronParser) Parse(expression string, timezone string) (scheduler.CronSchedule, error) {
	return &stubCronSchedule{}, nil
}

type stubCronSchedule struct{}

func (s *stubCronSchedule) Next(after time.Time) time.Time {
	return after.Add(time.Hour)
}
