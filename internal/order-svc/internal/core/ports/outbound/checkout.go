package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrCheckoutSnapshotNotFound           = errors.New("checkout snapshot not found")
	ErrCheckoutIdempotencyPayloadMismatch = errors.New("checkout idempotency payload mismatch")
	ErrStockReservationSKUNotFound        = errors.New("stock reservation sku not found")
	ErrStockReservationUnavailable        = errors.New("stock reservation unavailable")
	ErrStockReservationConflict           = errors.New("stock reservation conflict")
	ErrStockReleaseNotFound               = errors.New("stock release reservation not found")
	ErrStockReleaseUnavailable            = errors.New("stock release unavailable")
	ErrStockReleaseConflict               = errors.New("stock release conflict")
	ErrPaymentDeclined                    = errors.New("payment declined")
	ErrPaymentConflict                    = errors.New("payment conflict")
)

type CheckoutIdempotencyPayload struct {
	PaymentMethod string
}

type ValidateCheckoutIdempotencyInput struct {
	UserID         uuid.UUID
	IdempotencyKey string
	Payload        CheckoutIdempotencyPayload
}

type CheckoutSnapshot struct {
	UserID      uuid.UUID
	Currency    string
	TotalAmount int64
	Items       []CheckoutSnapshotItem
}

type CheckoutSnapshotItem struct {
	ProductID uuid.UUID
	SKU       string
	Name      string
	Quantity  int32
	UnitPrice int64
	LineTotal int64
	Currency  string
}

type ReserveStockInput struct {
	OrderID uuid.UUID
	UserID  uuid.UUID
	Items   []ReserveStockItem
}

type ReserveStockItem struct {
	ProductID uuid.UUID
	SKU       string
	Quantity  int32
}

type ReleaseStockInput struct {
	OrderID uuid.UUID
	UserID  uuid.UUID
	Items   []ReleaseStockItem
}

type ReleaseStockItem struct {
	ProductID uuid.UUID
	SKU       string
	Quantity  int32
}

type InitiatePaymentInput struct {
	OrderID         uuid.UUID
	Amount          int64
	Currency        string
	IdempotencyKey  string
	PaymentProvider string
}

//mockery:generate: true
type CheckoutSnapshotRepository interface {
	GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (CheckoutSnapshot, error)
}

//mockery:generate: true
type StockReservationService interface {
	ReserveStock(ctx context.Context, input ReserveStockInput) error
}

//mockery:generate: true
type StockReleaseService interface {
	ReleaseStock(ctx context.Context, input ReleaseStockInput) error
}

//mockery:generate: true
type CheckoutPaymentService interface {
	InitiatePayment(ctx context.Context, input InitiatePaymentInput) error
}

//mockery:generate: true
type CheckoutIdempotencyGuard interface {
	ValidateCheckoutIdempotency(ctx context.Context, input ValidateCheckoutIdempotencyInput) error
}
