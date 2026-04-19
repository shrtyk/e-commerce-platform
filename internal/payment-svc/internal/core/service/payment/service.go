package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

var (
	ErrPaymentAttemptDuplicate = errors.New("payment payment attempt duplicate")
	ErrInvalidPaymentInput     = errors.New("payment invalid payment input")

	errPaymentAttemptsRepoRequired = errors.New("payment attempts repository is required")
)

type InitiatePaymentInput struct {
	OrderID        uuid.UUID
	Amount         int64
	Currency       string
	ProviderName   string
	IdempotencyKey string
}

type InitiatePaymentResult struct {
	PaymentAttempt domain.PaymentAttempt
}

type Service struct {
	paymentAttempts outbound.PaymentAttemptRepository
	publisher       outbound.EventPublisher
	serviceName     string
}

func NewService(
	paymentAttempts outbound.PaymentAttemptRepository,
	publisher outbound.EventPublisher,
	serviceName string,
) *Service {
	return &Service{
		paymentAttempts: paymentAttempts,
		publisher:       publisher,
		serviceName:     serviceName,
	}
}

func (s *Service) InitiatePayment(
	ctx context.Context,
	input InitiatePaymentInput,
) (InitiatePaymentResult, error) {
	if s == nil || s.paymentAttempts == nil {
		return InitiatePaymentResult{}, errPaymentAttemptsRepoRequired
	}

	attempt, err := s.paymentAttempts.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{
		OrderID:        input.OrderID,
		Amount:         input.Amount,
		Currency:       input.Currency,
		ProviderName:   input.ProviderName,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, outbound.ErrPaymentAttemptDuplicate):
			return InitiatePaymentResult{}, fmt.Errorf("create payment attempt: %w", ErrPaymentAttemptDuplicate)
		case errors.Is(err, outbound.ErrInvalidPaymentAttemptArg):
			return InitiatePaymentResult{}, fmt.Errorf("create payment attempt: %w", ErrInvalidPaymentInput)
		default:
			return InitiatePaymentResult{}, fmt.Errorf("create payment attempt: %w", err)
		}
	}

	return InitiatePaymentResult{PaymentAttempt: attempt}, nil
}
