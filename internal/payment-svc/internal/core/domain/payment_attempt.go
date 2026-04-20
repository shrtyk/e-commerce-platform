package domain

import (
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string

const (
	PaymentStatusUnknown    PaymentStatus = "unknown"
	PaymentStatusInitiated  PaymentStatus = "initiated"
	PaymentStatusProcessing PaymentStatus = "processing"
	PaymentStatusSucceeded  PaymentStatus = "succeeded"
	PaymentStatusFailed     PaymentStatus = "failed"
)

type PaymentAttempt struct {
	PaymentAttemptID  uuid.UUID
	OrderID           uuid.UUID
	Status            PaymentStatus
	Amount            int64
	Currency          string
	ProviderName      string
	ProviderReference string
	IdempotencyKey    string
	FailureCode       string
	FailureMessage    string
	ProcessedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PaymentInitiatedPayload struct {
	PaymentAttemptID string
	OrderID          string
	Status           PaymentStatus
	Amount           int64
	Currency         string
	ProviderName     string
}

type PaymentSucceededPayload struct {
	PaymentAttemptID  string
	OrderID           string
	Status            PaymentStatus
	Amount            int64
	Currency          string
	ProviderName      string
	ProviderReference string
	ProcessedAt       *time.Time
}

type PaymentFailedPayload struct {
	PaymentAttemptID string
	OrderID          string
	Status           PaymentStatus
	Amount           int64
	Currency         string
	ProviderName     string
	FailureCode      string
	FailureMessage   string
	ProcessedAt      *time.Time
}

type DomainEvent struct {
	EventID       string
	EventName     string
	Producer      string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	SchemaVersion string
	AggregateType string
	AggregateID   string
	Topic         string
	Key           string
	Payload       any
	Headers       map[string]string
}
