package outbound

import (
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	OrderStatusPending         OrderStatus = "pending"
	OrderStatusAwaitingStock   OrderStatus = "awaiting_stock"
	OrderStatusAwaitingPayment OrderStatus = "awaiting_payment"
	OrderStatusConfirmed       OrderStatus = "confirmed"
	OrderStatusCancelled       OrderStatus = "cancelled"
)

type SagaStage string

const (
	SagaStageNotStarted SagaStage = "not_started"
	SagaStageRequested  SagaStage = "requested"
	SagaStageSucceeded  SagaStage = "succeeded"
	SagaStageFailed     SagaStage = "failed"
)

type Order struct {
	OrderID     uuid.UUID
	UserID      uuid.UUID
	Status      OrderStatus
	Currency    string
	TotalAmount int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Items       []OrderItem
}

type OrderItem struct {
	OrderItemID uuid.UUID
	OrderID     uuid.UUID
	ProductID   uuid.UUID
	SKU         string
	Name        string
	Quantity    int32
	UnitPrice   int64
	LineTotal   int64
	Currency    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateOrderInput struct {
	OrderID        uuid.UUID
	UserID         uuid.UUID
	Status         OrderStatus
	Currency       string
	TotalAmount    int64
	IdempotencyKey string
	Items          []CreateOrderItemInput
}

type CreateOrderItemInput struct {
	ProductID uuid.UUID
	SKU       string
	Name      string
	Quantity  int32
	UnitPrice int64
	LineTotal int64
	Currency  string
}

type OrderStatusHistory struct {
	OrderStatusHistoryID uuid.UUID
	OrderID              uuid.UUID
	FromStatus           *OrderStatus
	ToStatus             OrderStatus
	ReasonCode           *string
	CreatedAt            time.Time
}

type SagaState struct {
	OrderID       uuid.UUID
	StockStage    SagaStage
	PaymentStage  SagaStage
	LastErrorCode *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
