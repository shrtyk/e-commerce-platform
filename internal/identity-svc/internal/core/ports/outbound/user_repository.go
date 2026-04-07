package outbound

import (
	"context"
	"errors"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

var (
	ErrUserNotFound   = errors.New("identity user not found")
	ErrDuplicateEmail = errors.New("identity duplicate email")
)

//mockery:generate: true
type UserRepository interface {
	Create(ctx context.Context, user domain.User) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}
