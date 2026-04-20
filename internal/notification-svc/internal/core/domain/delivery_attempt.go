package domain

import (
	"time"

	"github.com/google/uuid"
)

type DeliveryAttempt struct {
	DeliveryAttemptID uuid.UUID
	DeliveryRequestID uuid.UUID
	AttemptNumber     int32
	ProviderName      string
	ProviderMessageID string
	FailureCode       string
	FailureMessage    string
	AttemptedAt       time.Time
}
