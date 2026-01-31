package domain

import (
	"time"

	"github.com/google/uuid"
)

type Schedule struct {
	ID uuid.UUID

	CronExpression string
	Timezone       string // IANA timezone, defaults to UTC

	CreatedAt time.Time
	UpdatedAt time.Time
}
