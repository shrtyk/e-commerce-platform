package outbound

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

var ErrSessionNotFound = errors.New("identity session not found")

//mockery:generate: true
type SessionRepository interface {
	Create(ctx context.Context, session domain.Session) (domain.Session, error)
	GetByID(ctx context.Context, sessionID uuid.UUID) (domain.Session, error)
	Revoke(ctx context.Context, sessionID uuid.UUID, revokedAt time.Time) error
}
