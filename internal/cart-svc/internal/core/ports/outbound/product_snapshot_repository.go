package outbound

import (
	"context"
	"errors"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

var (
	ErrProductSnapshotNotFound = errors.New("product snapshot not found")
	ErrProductSnapshotConflict = errors.New("product snapshot conflict")
	ErrProductNotFound         = errors.New("product not found")
)

//mockery:generate: true
type ProductSnapshotRepository interface {
	GetBySKU(ctx context.Context, sku string) (domain.ProductSnapshot, error)
	Upsert(ctx context.Context, snapshot domain.ProductSnapshot) (domain.ProductSnapshot, error)
}
