package cart

import (
	"errors"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
)

type CartService struct {
	carts     outbound.CartRepository
	items     outbound.CartItemRepository
	snapshots outbound.ProductSnapshotRepository
}

func NewCartService(
	carts outbound.CartRepository,
	items outbound.CartItemRepository,
	snapshots outbound.ProductSnapshotRepository,
) *CartService {
	return &CartService{
		carts:     carts,
		items:     items,
		snapshots: snapshots,
	}
}

var (
	ErrInvalidUserID           = errors.New("cart invalid user id")
	ErrInvalidSKU              = errors.New("cart invalid sku")
	ErrInvalidQuantity         = errors.New("cart invalid quantity")
	ErrCartCurrencyMismatch    = errors.New("cart currency mismatch")
	ErrCartNotFound            = errors.New("cart not found")
	ErrCartItemNotFound        = errors.New("cart item not found")
	ErrCartItemAlreadyExists   = errors.New("cart item already exists")
	ErrProductSnapshotNotFound = errors.New("product snapshot not found")
)
