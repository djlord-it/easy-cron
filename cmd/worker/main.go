package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"easycron/internal/dispatcher"
	"easycron/internal/domain"
	"easycron/internal/scheduler"
	"easycron/internal/transport/channel"
)

func main() {
	// TODO: replace with real implementations
	store := &stubStore{}
	cronParser := &stubCronParser{}

	bus := channel.NewEventBus(100)
	webhookSender := dispatcher.NewHTTPWebhookSender()

	sched := scheduler.New(
		scheduler.Config{TickInterval: 30 * time.Second},
		store,
		cronParser,
		bus,
	)

	disp := dispatcher.New(store, webhookSender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Run(ctx)
	go disp.Run(ctx, bus.Channel())

	log.Println("worker: started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("worker: shutting down")
	cancel()
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
