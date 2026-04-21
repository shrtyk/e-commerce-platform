package domain

import "time"

type DeliveryRequestedPayload struct {
	DeliveryRequestID string
	SourceEventName   string
	Channel           string
	Recipient         string
	TemplateKey       string
	Status            DeliveryStatus
}

type NotificationSentPayload struct {
	DeliveryRequestID string
	SourceEventName   string
	Channel           string
	Recipient         string
	Status            DeliveryStatus
	SentAt            time.Time
}

type NotificationFailedPayload struct {
	DeliveryRequestID string
	SourceEventName   string
	Channel           string
	Recipient         string
	Status            DeliveryStatus
	FailureCode       string
	FailureMessage    string
	FailedAt          time.Time
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
