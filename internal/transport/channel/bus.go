package channel

import (
	"context"
	"errors"
	"time"

	"easycron/internal/domain"
)

// ErrBufferFull is returned when the event buffer is full and emit times out.
var ErrBufferFull = errors.New("event buffer full")

// DefaultEmitTimeout is the default timeout for emit operations.
const DefaultEmitTimeout = 5 * time.Second

type EventBus struct {
	ch          chan domain.TriggerEvent
	emitTimeout time.Duration
}

// Option configures an EventBus.
type Option func(*EventBus)

// WithEmitTimeout sets the timeout for emit operations.
func WithEmitTimeout(d time.Duration) Option {
	return func(b *EventBus) {
		b.emitTimeout = d
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
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrBufferFull
	}
}

func (b *EventBus) Channel() <-chan domain.TriggerEvent {
	return b.ch
}
