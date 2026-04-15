package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

var (
	ErrCartItemNotFound      = errors.New("cart item not found")
	ErrCartItemAlreadyExists = errors.New("cart item already exists")
)

//mockery:generate: true
type CartItemRepository interface {
	ListByCartID(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error)
	Insert(ctx context.Context, params CartItemInsertParams) (domain.CartItem, error)
	UpdateQuantity(ctx context.Context, cartID uuid.UUID, sku string, quantity int64) (domain.CartItem, error)
	Delete(ctx context.Context, cartID uuid.UUID, sku string) error
}

type CartItemInsertParams struct {
	CartID      uuid.UUID
	SKU         string
	Quantity    int64
	UnitPrice   int64
	Currency    string
	ProductName string
}
