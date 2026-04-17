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
	ErrPaymentDeclined                    = errors.New("payment declined")
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

//mockery:generate: true
type CheckoutSnapshotRepository interface {
	GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (CheckoutSnapshot, error)
}

//mockery:generate: true
type StockReservationService interface {
	ReserveStock(ctx context.Context, input ReserveStockInput) error
}

//mockery:generate: true
type CheckoutIdempotencyGuard interface {
	ValidateCheckoutIdempotency(ctx context.Context, input ValidateCheckoutIdempotencyInput) error
}
