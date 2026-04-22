package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
)

var (
	ErrProductNotFound      = errors.New("product not found")
	ErrProductAlreadyExists = errors.New("product already exists")
	ErrInvalidCurrency      = errors.New("invalid currency")
)

//mockery:generate: true
type ProductRepository interface {
	GetByID(ctx context.Context, productID uuid.UUID) (domain.Product, error)
	GetBySKU(ctx context.Context, sku string) (domain.Product, error)
	GetCurrencyByCode(ctx context.Context, code string) (uuid.UUID, error)
	List(ctx context.Context, params ProductListParams) ([]domain.Product, error)
	Create(ctx context.Context, product domain.Product) (domain.Product, error)
	Update(ctx context.Context, product domain.Product) (domain.Product, error)
	Delete(ctx context.Context, productID uuid.UUID) (domain.Product, error)
}

type ProductListParams struct {
	Limit  int32
	Offset int32
}
