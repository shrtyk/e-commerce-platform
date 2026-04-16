package cart

import (
	"errors"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
)

type CartService struct {
	carts     outbound.CartRepository
	items     outbound.CartItemRepository
	snapshots outbound.ProductSnapshotRepository
	catalog   outbound.CatalogReader
	cache     outbound.CartCache
	cacheTTL  time.Duration
}

const defaultActiveCartCacheTTL = 5 * time.Minute

func NewCartService(
	carts outbound.CartRepository,
	items outbound.CartItemRepository,
	snapshots outbound.ProductSnapshotRepository,
) *CartService {
	return NewCartServiceWithCatalogAndCache(carts, items, snapshots, nil, nil, 0)
}

func NewCartServiceWithCatalog(
	carts outbound.CartRepository,
	items outbound.CartItemRepository,
	snapshots outbound.ProductSnapshotRepository,
	catalog outbound.CatalogReader,
) *CartService {
	return NewCartServiceWithCatalogAndCache(carts, items, snapshots, catalog, nil, 0)
}

func NewCartServiceWithCatalogAndCache(
	carts outbound.CartRepository,
	items outbound.CartItemRepository,
	snapshots outbound.ProductSnapshotRepository,
	catalog outbound.CatalogReader,
	cache outbound.CartCache,
	cacheTTL time.Duration,
) *CartService {
	if cacheTTL <= 0 {
		cacheTTL = defaultActiveCartCacheTTL
	}

	return &CartService{
		carts:     carts,
		items:     items,
		snapshots: snapshots,
		catalog:   catalog,
		cache:     cache,
		cacheTTL:  cacheTTL,
	}
}

var (
	ErrInvalidUserID           = errors.New("cart invalid user id")
	ErrInvalidSKU              = errors.New("cart invalid sku")
	ErrInvalidQuantity         = errors.New("cart invalid quantity")
	ErrInvalidCatalogProductID = errors.New("cart invalid catalog product id")
	ErrCartCurrencyMismatch    = errors.New("cart currency mismatch")
	ErrCartNotFound            = errors.New("cart not found")
	ErrCartItemNotFound        = errors.New("cart item not found")
	ErrCartItemAlreadyExists   = errors.New("cart item already exists")
	ErrProductSnapshotNotFound = errors.New("product snapshot not found")
)
