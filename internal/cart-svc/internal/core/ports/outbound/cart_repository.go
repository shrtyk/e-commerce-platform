package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
)

var (
	ErrCartNotFound      = errors.New("cart not found")
	ErrCartAlreadyExists = errors.New("cart already exists")
)

//mockery:generate: true
type CartRepository interface {
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	CreateActive(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error)
}
