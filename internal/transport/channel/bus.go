package channel

import (
	"context"
	"errors"
	"time"

	"github.com/djlord-it/easy-cron/internal/domain"
)

// ErrBufferFull is returned when the event buffer is full and emit times out.
var ErrBufferFull = errors.New("event buffer full")

// DefaultEmitTimeout is the default timeout for emit operations.
const DefaultEmitTimeout = 5 * time.Second

// MetricsSink defines the interface for recording event bus metrics.
// All methods must be non-blocking and fire-and-forget.
type MetricsSink interface {
	BufferSizeUpdate(size int)
	EmitError()
}

type EventBus struct {
	ch          chan domain.TriggerEvent
	emitTimeout time.Duration
	metrics     MetricsSink // optional, nil = disabled
}

// Option configures an EventBus.
type Option func(*EventBus)

// WithEmitTimeout sets the timeout for emit operations.
func WithEmitTimeout(d time.Duration) Option {
	return func(b *EventBus) {
		b.emitTimeout = d
	}
}

// WithMetrics attaches a metrics sink to the event bus.
func WithMetrics(sink MetricsSink) Option {
	return func(b *EventBus) {
		b.metrics = sink
	}
}

func NewEventBus(buffer int, opts ...Option) *EventBus {
	b := &EventBus{
		ch:          make(chan domain.TriggerEvent, buffer),
		emitTimeout: DefaultEmitTimeout,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *EventBus) Emit(ctx context.Context, event domain.TriggerEvent) error {
	timer := time.NewTimer(b.emitTimeout)
	defer timer.Stop()

	select {
	case b.ch <- event:
		// Record buffer size after successful emit
		if b.metrics != nil {
			b.metrics.BufferSizeUpdate(len(b.ch))
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		// Record emit error
		if b.metrics != nil {
			b.metrics.EmitError()
		}
		return ErrBufferFull
	}
}

func (b *EventBus) Channel() <-chan domain.TriggerEvent {
	return b.ch
}
