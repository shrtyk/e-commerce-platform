package outbound

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
)

var (
	ErrPaymentAttemptNotFound   = errors.New("payment payment attempt not found")
	ErrPaymentAttemptDuplicate  = errors.New("payment payment attempt duplicate")
	ErrInvalidPaymentAttemptArg = errors.New("payment invalid payment attempt arg")
)

type CreatePaymentAttemptInput struct {
	OrderID        uuid.UUID
	Amount         int64
	Currency       string
	ProviderName   string
	IdempotencyKey string
}

//mockery:generate: true
type PaymentAttemptRepository interface {
	CreateInitiated(ctx context.Context, input CreatePaymentAttemptInput) (domain.PaymentAttempt, error)
}
