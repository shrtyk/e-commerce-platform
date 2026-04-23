package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrConsumerIdempotencyDuplicate  = errors.New("order consumer idempotency duplicate")
	ErrInvalidConsumerIdempotencyArg = errors.New("order invalid consumer idempotency arg")
)

type CreateConsumerIdempotencyInput struct {
	EventID           uuid.UUID
	ConsumerGroupName string
}

//mockery:generate: true
type ConsumerIdempotencyRepository interface {
	Create(ctx context.Context, input CreateConsumerIdempotencyInput) error
	Exists(ctx context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error)
}
