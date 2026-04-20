package domain

import (
	"time"

	"github.com/google/uuid"
)

type DeliveryStatus string

const (
	DeliveryStatusUnknown   DeliveryStatus = ""
	DeliveryStatusRequested DeliveryStatus = "requested"
	DeliveryStatusSent      DeliveryStatus = "sent"
	DeliveryStatusFailed    DeliveryStatus = "failed"
)

type DeliveryRequest struct {
	DeliveryRequestID uuid.UUID
	SourceEventID     uuid.UUID
	SourceEventName   string
	Channel           string
	Recipient         string
	TemplateKey       string
	Status            DeliveryStatus
	IdempotencyKey    string
	LastErrorCode     string
	LastErrorMessage  string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
