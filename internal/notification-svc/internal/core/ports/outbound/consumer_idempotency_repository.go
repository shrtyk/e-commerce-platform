package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrConsumerIdempotencyDuplicate  = errors.New("notification consumer idempotency duplicate")
	ErrInvalidConsumerIdempotencyArg = errors.New("notification invalid consumer idempotency arg")
)

type CreateConsumerIdempotencyInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
	DeliveryRequestID uuid.UUID
}

//mockery:generate: true
type ConsumerIdempotencyRepository interface {
	Create(ctx context.Context, input CreateConsumerIdempotencyInput) error
	Exists(ctx context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error)
}
