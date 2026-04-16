package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

//mockery:generate: true
type CartCache interface {
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, bool, error)
	SetActiveByUserID(ctx context.Context, userID uuid.UUID, cart domain.Cart, ttl time.Duration) error
	DeleteActiveByUserID(ctx context.Context, userID uuid.UUID) error
}
