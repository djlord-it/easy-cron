package domain

import (
	"time"

	"github.com/google/uuid"
)

type ExecutionStatus string

const (
	ExecutionStatusEmitted   ExecutionStatus = "emitted"
	ExecutionStatusDelivered ExecutionStatus = "delivered"
	ExecutionStatusFailed    ExecutionStatus = "failed"
)

// Execution records that a job fired at a specific time.
type Execution struct {
	ID uuid.UUID

	JobID     uuid.UUID
	ProjectID uuid.UUID

	ScheduledAt time.Time
	FiredAt     time.Time
	Status      ExecutionStatus

	CreatedAt time.Time
}
