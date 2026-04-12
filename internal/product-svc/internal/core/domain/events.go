package domain

import "time"

type ProductCreatedPayload struct {
	ProductID  string
	SKU        string
	Name       string
	Status     ProductStatus
	Price      int64
	Currency   string
	CategoryID string
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
