package domain

import "time"

type OrderItemSnapshot struct {
	ProductID string
	SKU       string
	Name      string
	Quantity  int32
	UnitPrice int64
	LineTotal int64
	Currency  string
}

type OrderCreatedPayload struct {
	OrderID     string
	UserID      string
	Status      OrderStatus
	Currency    string
	TotalAmount int64
	Items       []OrderItemSnapshot
}

type OrderCancelledPayload struct {
	OrderID             string
	UserID              string
	Status              OrderStatus
	CancelReasonCode    string
	CancelReasonMessage string
	CancelledAt         time.Time
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
