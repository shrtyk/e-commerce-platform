package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
)

var (
	ErrDeliveryRequestNotFound   = errors.New("notification delivery request not found")
	ErrDeliveryRequestDuplicate  = errors.New("notification delivery request duplicate")
	ErrInvalidDeliveryRequestArg = errors.New("notification invalid delivery request arg")
)

type CreateDeliveryRequestInput struct {
	SourceEventID   uuid.UUID
	SourceEventName string
	Channel         string
	Recipient       string
	TemplateKey     string
	IdempotencyKey  string
}

//mockery:generate: true
type DeliveryRequestRepository interface {
	CreateRequested(ctx context.Context, input CreateDeliveryRequestInput) (domain.DeliveryRequest, error)
	GetByID(ctx context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error)
	GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (domain.DeliveryRequest, error)
	MarkSent(ctx context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error)
	MarkFailed(ctx context.Context, deliveryRequestID uuid.UUID, failureCode string, failureMessage string) (domain.DeliveryRequest, error)
}
