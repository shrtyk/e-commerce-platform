package outbound

import (
	"context"
	"errors"

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
	Update(ctx context.Context, stock domain.StockRecord) (domain.StockRecord, error)
}
