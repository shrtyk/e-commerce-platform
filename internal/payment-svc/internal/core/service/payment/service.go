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
	errPaymentProviderRequired     = errors.New("payment provider is required")
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
	provider        outbound.PaymentProvider
	serviceName     string
}

func NewService(
	paymentAttempts outbound.PaymentAttemptRepository,
	publisher outbound.EventPublisher,
	provider outbound.PaymentProvider,
	serviceName string,
) *Service {
	return &Service{
		paymentAttempts: paymentAttempts,
		publisher:       publisher,
		provider:        provider,
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

	if s.provider == nil {
		return InitiatePaymentResult{}, errPaymentProviderRequired
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
			existingAttempt, getErr := s.paymentAttempts.GetByOrderIDAndIdempotencyKey(ctx, input.OrderID, input.IdempotencyKey)
			if getErr != nil {
				if errors.Is(getErr, outbound.ErrPaymentAttemptNotFound) {
					return InitiatePaymentResult{}, fmt.Errorf("get existing payment attempt after duplicate: %w", ErrPaymentAttemptDuplicate)
				}

				return InitiatePaymentResult{}, fmt.Errorf("get existing payment attempt after duplicate: %w", getErr)
			}

			attempt = existingAttempt
		case errors.Is(err, outbound.ErrInvalidPaymentAttemptArg):
			return InitiatePaymentResult{}, fmt.Errorf("create payment attempt: %w", ErrInvalidPaymentInput)
		default:
			return InitiatePaymentResult{}, fmt.Errorf("create payment attempt: %w", err)
		}
	}

	if attempt.Status == domain.PaymentStatusSucceeded || attempt.Status == domain.PaymentStatusFailed {
		return InitiatePaymentResult{PaymentAttempt: attempt}, nil
	}

	if attempt.Status == domain.PaymentStatusProcessing {
		return InitiatePaymentResult{PaymentAttempt: attempt}, nil
	}

	processingAttempt, err := s.paymentAttempts.MarkProcessing(ctx, attempt.PaymentAttemptID)
	if err != nil {
		return InitiatePaymentResult{}, fmt.Errorf("mark payment attempt processing: %w", err)
	}

	chargeResult, err := s.provider.Charge(ctx, outbound.ChargePaymentInput{
		PaymentAttemptID: processingAttempt.PaymentAttemptID,
		OrderID:          processingAttempt.OrderID,
		Amount:           processingAttempt.Amount,
		Currency:         processingAttempt.Currency,
		ProviderName:     processingAttempt.ProviderName,
		IdempotencyKey:   processingAttempt.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, outbound.ErrPaymentDeclined) {
			failedAttempt, markErr := s.paymentAttempts.MarkFailed(
				ctx,
				processingAttempt.PaymentAttemptID,
				chargeResult.FailureCode,
				chargeResult.FailureMessage,
			)
			if markErr != nil {
				return InitiatePaymentResult{}, fmt.Errorf("mark payment attempt failed: %w", markErr)
			}

			return InitiatePaymentResult{PaymentAttempt: failedAttempt}, nil
		}

		return InitiatePaymentResult{}, fmt.Errorf("charge payment: %w", err)
	}

	succeededAttempt, err := s.paymentAttempts.MarkSucceeded(
		ctx,
		processingAttempt.PaymentAttemptID,
		chargeResult.ProviderReference,
	)
	if err != nil {
		return InitiatePaymentResult{}, fmt.Errorf("mark payment attempt succeeded: %w", err)
	}

	return InitiatePaymentResult{PaymentAttempt: succeededAttempt}, nil
}
