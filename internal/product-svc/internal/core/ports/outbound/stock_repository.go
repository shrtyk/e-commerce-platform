package outbound

import (
	"context"
	"errors"
    "time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
)

var (
	ErrStockRecordNotFound = errors.New("catalog stock record not found")
	ErrInvalidStockUpdate  = errors.New("catalog invalid stock update")
)

//mockery:generate: true
type StockRepository interface {
	Create(ctx context.Context, stock domain.StockRecord) (domain.StockRecord, error)
	GetByProductID(ctx context.Context, productID uuid.UUID) (domain.StockRecord, error)
	GetByProductIDForUpdate(ctx context.Context, productID uuid.UUID) (domain.StockRecord, error)
	Update(ctx context.Context, stock domain.StockRecord) (domain.StockRecord, error)
	CreateReservation(ctx context.Context, reservation StockReservation) (StockReservation, error)
	ListReservationsByOrderID(ctx context.Context, orderID uuid.UUID) ([]StockReservation, error)
	DeleteReservationsByOrderID(ctx context.Context, orderID uuid.UUID) error
}

type StockReservation struct {
	StockReservationID uuid.UUID
	OrderID            uuid.UUID
	ProductID          uuid.UUID
	Quantity           int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
