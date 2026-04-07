package outbound

import (
	"context"
	"errors"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

var ErrSessionNotFound = errors.New("identity session not found")

//mockery:generate: true
type SessionRepository interface {
	Create(ctx context.Context, session domain.Session) (domain.Session, error)
	GetByID(ctx context.Context, sessionID string) (domain.Session, error)
}
