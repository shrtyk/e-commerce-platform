package outbound

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
)

var (
	ErrDeliveryAttemptDuplicate  = errors.New("notification delivery attempt duplicate")
	ErrInvalidDeliveryAttemptArg = errors.New("notification invalid delivery attempt arg")
)

type CreateDeliveryAttemptInput struct {
	DeliveryRequestID uuid.UUID
	AttemptNumber     int32
	ProviderName      string
	ProviderMessageID string
	FailureCode       string
	FailureMessage    string
	AttemptedAt       time.Time
}

//mockery:generate: true
type DeliveryAttemptRepository interface {
	Create(ctx context.Context, input CreateDeliveryAttemptInput) (domain.DeliveryAttempt, error)
	ListByDeliveryRequestID(ctx context.Context, deliveryRequestID uuid.UUID) ([]domain.DeliveryAttempt, error)
}
