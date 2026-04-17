package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrOrderNotFound                = errors.New("order not found")
	ErrOrderAlreadyExists           = errors.New("order already exists")
	ErrOrderIdempotencyConflict     = errors.New("order idempotency conflict")
	ErrOrderInvalidStatusTransition = errors.New("order invalid status transition")
)

//mockery:generate: true
type OrderRepository interface {
	CreateWithItems(ctx context.Context, input CreateOrderInput) (Order, error)
	GetByID(ctx context.Context, orderID uuid.UUID) (Order, error)
	GetByUserIDAndIdempotencyKey(ctx context.Context, userID uuid.UUID, idempotencyKey string) (Order, error)
	TransitionStatus(ctx context.Context, orderID uuid.UUID, fromStatus OrderStatus, toStatus OrderStatus) (Order, error)
	AppendStatusHistory(ctx context.Context, orderID uuid.UUID, fromStatus *OrderStatus, toStatus OrderStatus, reasonCode *string) (OrderStatusHistory, error)
}
