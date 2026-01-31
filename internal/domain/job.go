package domain

import (
	"time"

	"github.com/google/uuid"
)

type Job struct {
	ID        uuid.UUID
	ProjectID uuid.UUID

	Name    string
	Enabled bool

	ScheduleID uuid.UUID
	Delivery   DeliveryConfig

	CreatedAt time.Time
	UpdatedAt time.Time
}
