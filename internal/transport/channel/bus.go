package channel

import (
	"context"

	"easycron/internal/domain"
)

type EventBus struct {
	ch chan domain.TriggerEvent
}

func NewEventBus(buffer int) *EventBus {
	return &EventBus{
		ch: make(chan domain.TriggerEvent, buffer),
	}
}

func (b *EventBus) Emit(ctx context.Context, event domain.TriggerEvent) error {
	select {
	case b.ch <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *EventBus) Channel() <-chan domain.TriggerEvent {
	return b.ch
}
