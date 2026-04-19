package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

func TestServiceInitiatePayment(t *testing.T) {
	t.Run("returns invalid arg when repository missing", func(t *testing.T) {
		svc := NewService(nil, nil, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{})

		require.ErrorContains(t, err, "payment attempts repository is required")
	})

	t.Run("delegates to repository", func(t *testing.T) {
		orderID := uuid.New()
		want := domain.PaymentAttempt{PaymentAttemptID: uuid.New(), OrderID: orderID, Status: domain.PaymentStatusInitiated}

		repo := stubPaymentAttemptRepository{
			createInitiatedFunc: func(_ context.Context, input outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				require.Equal(t, orderID, input.OrderID)
				return want, nil
			},
		}

		svc := NewService(repo, nil, "payment-svc")
		got, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: orderID, Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-1"})

		require.NoError(t, err)
		require.Equal(t, want, got.PaymentAttempt)
	})

	t.Run("maps outbound duplicate error", func(t *testing.T) {
		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
		}, nil, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: uuid.New(), Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-dup"})

		require.ErrorIs(t, err, ErrPaymentAttemptDuplicate)
	})

	t.Run("maps outbound invalid arg error", func(t *testing.T) {
		svc := NewService(stubPaymentAttemptRepository{
			createInitiatedFunc: func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
				return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
			},
		}, nil, "payment-svc")

		_, err := svc.InitiatePayment(context.Background(), InitiatePaymentInput{OrderID: uuid.New(), Amount: 100, Currency: "USD", ProviderName: "stub", IdempotencyKey: "id-bad"})

		require.ErrorIs(t, err, ErrInvalidPaymentInput)
	})
}

type stubPaymentAttemptRepository struct {
	createInitiatedFunc func(context.Context, outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error)
}

func (s stubPaymentAttemptRepository) CreateInitiated(ctx context.Context, input outbound.CreatePaymentAttemptInput) (domain.PaymentAttempt, error) {
	if s.createInitiatedFunc == nil {
		return domain.PaymentAttempt{}, errors.New("create initiated not configured")
	}

	return s.createInitiatedFunc(ctx, input)
}
