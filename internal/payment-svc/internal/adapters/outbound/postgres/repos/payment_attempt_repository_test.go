package repos

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

func TestPaymentAttemptRepositoryCreateInitiated(t *testing.T) {
	ctx := context.Background()

	orderID := uuid.New()
	attemptID := uuid.New()
	now := time.Now().UTC()

	t.Run("creates initiated payment attempt", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			createInitiatedPaymentAttemptFunc: func(_ context.Context, arg sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error) {
				require.Equal(t, orderID, arg.OrderID)
				require.Equal(t, sqlc.PaymentStatusInitiated, arg.Status)
				require.Equal(t, int64(1500), arg.Amount)
				require.Equal(t, "USD", arg.Currency)
				require.Equal(t, "stub", arg.ProviderName)
				require.Equal(t, "idem-1", arg.IdempotencyKey)

				return sqlc.PaymentAttempt{
					PaymentAttemptID:  attemptID,
					OrderID:           orderID,
					Status:            sqlc.PaymentStatusInitiated,
					Amount:            1500,
					Currency:          "USD",
					ProviderName:      "stub",
					ProviderReference: "",
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		attempt, err := repo.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{
			OrderID:        orderID,
			Amount:         1500,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "idem-1",
		})

		require.NoError(t, err)
		require.Equal(t, domain.PaymentAttempt{
			PaymentAttemptID:  attemptID,
			OrderID:           orderID,
			Status:            domain.PaymentStatusInitiated,
			Amount:            1500,
			Currency:          "USD",
			ProviderName:      "stub",
			ProviderReference: "",
			IdempotencyKey:    "idem-1",
			CreatedAt:         now,
			UpdatedAt:         now,
		}, attempt)
	})

	t.Run("returns invalid input error for bad args", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{})

		_, err := repo.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{})

		require.ErrorIs(t, err, outbound.ErrInvalidPaymentAttemptArg)
	})

	t.Run("returns invalid input error for nil order id", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{})

		_, err := repo.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{
			Amount:         1500,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "idem-1",
		})

		require.ErrorIs(t, err, outbound.ErrInvalidPaymentAttemptArg)
	})

	t.Run("wraps duplicate error", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			createInitiatedPaymentAttemptFunc: func(context.Context, sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error) {
				return sqlc.PaymentAttempt{}, outbound.ErrPaymentAttemptDuplicate
			},
		})

		_, err := repo.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{
			OrderID:        orderID,
			Amount:         100,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "idem-2",
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, outbound.ErrPaymentAttemptDuplicate))
	})
}

type stubPaymentAttemptQuerier struct {
	createInitiatedPaymentAttemptFunc func(context.Context, sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error)
}

func (s stubPaymentAttemptQuerier) CreateInitiatedPaymentAttempt(ctx context.Context, arg sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error) {
	if s.createInitiatedPaymentAttemptFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub create initiated payment attempt is not configured")
	}

	return s.createInitiatedPaymentAttemptFunc(ctx, arg)
}
