package repos

import (
	"context"
	"database/sql"
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

	t.Run("returns not found error", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			createInitiatedPaymentAttemptFunc: func(context.Context, sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error) {
				return sqlc.PaymentAttempt{}, sql.ErrNoRows
			},
		})

		_, err := repo.CreateInitiated(ctx, outbound.CreatePaymentAttemptInput{
			OrderID:        orderID,
			Amount:         100,
			Currency:       "USD",
			ProviderName:   "stub",
			IdempotencyKey: "idem-3",
		})

		require.Error(t, err)
		require.True(t, errors.Is(err, outbound.ErrPaymentAttemptNotFound))
	})
}

func TestPaymentAttemptRepositoryMarkProcessing(t *testing.T) {
	ctx := context.Background()
	paymentAttemptID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()

	t.Run("marks processing from initiated", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptProcessingFunc: func(_ context.Context, arg sqlc.MarkPaymentAttemptProcessingParams) (sqlc.PaymentAttempt, error) {
				require.Equal(t, paymentAttemptID, arg.PaymentAttemptID)
				require.Equal(t, sqlc.PaymentStatusProcessing, arg.Status)

				return sqlc.PaymentAttempt{
					PaymentAttemptID:  paymentAttemptID,
					OrderID:           orderID,
					Status:            sqlc.PaymentStatusProcessing,
					Amount:            100,
					Currency:          "USD",
					ProviderName:      "stub",
					ProviderReference: "",
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		attempt, err := repo.MarkProcessing(ctx, paymentAttemptID)

		require.NoError(t, err)
		require.Equal(t, domain.PaymentStatusProcessing, attempt.Status)
	})

	t.Run("maps guard no rows to not found", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptProcessingFunc: func(context.Context, sqlc.MarkPaymentAttemptProcessingParams) (sqlc.PaymentAttempt, error) {
				return sqlc.PaymentAttempt{}, sql.ErrNoRows
			},
		})

		_, err := repo.MarkProcessing(ctx, paymentAttemptID)

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrPaymentAttemptNotFound)
	})
}

func TestPaymentAttemptRepositoryMarkSucceeded(t *testing.T) {
	ctx := context.Background()
	paymentAttemptID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()

	t.Run("marks succeeded from processing", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptSucceededFunc: func(_ context.Context, arg sqlc.MarkPaymentAttemptSucceededParams) (sqlc.PaymentAttempt, error) {
				require.Equal(t, paymentAttemptID, arg.PaymentAttemptID)
				require.Equal(t, sqlc.PaymentStatusSucceeded, arg.Status)
				require.Equal(t, "provider-ref", arg.ProviderReference)

				return sqlc.PaymentAttempt{
					PaymentAttemptID:  paymentAttemptID,
					OrderID:           orderID,
					Status:            sqlc.PaymentStatusSucceeded,
					Amount:            100,
					Currency:          "USD",
					ProviderName:      "stub",
					ProviderReference: "provider-ref",
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		attempt, err := repo.MarkSucceeded(ctx, paymentAttemptID, "provider-ref")

		require.NoError(t, err)
		require.Equal(t, domain.PaymentStatusSucceeded, attempt.Status)
		require.Equal(t, "provider-ref", attempt.ProviderReference)
	})

	t.Run("maps guard no rows to not found", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptSucceededFunc: func(context.Context, sqlc.MarkPaymentAttemptSucceededParams) (sqlc.PaymentAttempt, error) {
				return sqlc.PaymentAttempt{}, sql.ErrNoRows
			},
		})

		_, err := repo.MarkSucceeded(ctx, paymentAttemptID, "provider-ref")

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrPaymentAttemptNotFound)
	})
}

func TestPaymentAttemptRepositoryMarkFailed(t *testing.T) {
	ctx := context.Background()
	paymentAttemptID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()

	t.Run("persists and maps failure reason", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptFailedFunc: func(_ context.Context, arg sqlc.MarkPaymentAttemptFailedParams) (sqlc.PaymentAttempt, error) {
				require.Equal(t, paymentAttemptID, arg.PaymentAttemptID)
				require.Equal(t, sqlc.PaymentStatusFailed, arg.Status)
				require.Equal(t, "declined", arg.FailureCode.String)
				require.True(t, arg.FailureCode.Valid)
				require.Equal(t, "issuer rejected", arg.FailureMessage.String)
				require.True(t, arg.FailureMessage.Valid)

				return sqlc.PaymentAttempt{
					PaymentAttemptID:  paymentAttemptID,
					OrderID:           orderID,
					Status:            sqlc.PaymentStatusFailed,
					Amount:            100,
					Currency:          "USD",
					ProviderName:      "stub",
					ProviderReference: "",
					IdempotencyKey:    "idem-1",
					FailureCode:       sql.NullString{String: "declined", Valid: true},
					FailureMessage:    sql.NullString{String: "issuer rejected", Valid: true},
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		attempt, err := repo.MarkFailed(ctx, paymentAttemptID, "declined", "issuer rejected")

		require.NoError(t, err)
		require.Equal(t, domain.PaymentStatusFailed, attempt.Status)
		require.Equal(t, "declined", attempt.FailureCode)
		require.Equal(t, "issuer rejected", attempt.FailureMessage)
	})

	t.Run("maps guard no rows to not found", func(t *testing.T) {
		repo := NewPaymentAttemptRepositoryFromQuerier(stubPaymentAttemptQuerier{
			markPaymentAttemptFailedFunc: func(context.Context, sqlc.MarkPaymentAttemptFailedParams) (sqlc.PaymentAttempt, error) {
				return sqlc.PaymentAttempt{}, sql.ErrNoRows
			},
		})

		_, err := repo.MarkFailed(ctx, paymentAttemptID, "declined", "issuer rejected")

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrPaymentAttemptNotFound)
	})
}

type stubPaymentAttemptQuerier struct {
	createInitiatedPaymentAttemptFunc               func(context.Context, sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error)
	getPaymentAttemptByOrderIDAndIdempotencyKeyFunc func(context.Context, sqlc.GetPaymentAttemptByOrderIDAndIdempotencyKeyParams) (sqlc.PaymentAttempt, error)
	markPaymentAttemptProcessingFunc                func(context.Context, sqlc.MarkPaymentAttemptProcessingParams) (sqlc.PaymentAttempt, error)
	markPaymentAttemptSucceededFunc                 func(context.Context, sqlc.MarkPaymentAttemptSucceededParams) (sqlc.PaymentAttempt, error)
	markPaymentAttemptFailedFunc                    func(context.Context, sqlc.MarkPaymentAttemptFailedParams) (sqlc.PaymentAttempt, error)
}

func (s stubPaymentAttemptQuerier) CreateInitiatedPaymentAttempt(ctx context.Context, arg sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error) {
	if s.createInitiatedPaymentAttemptFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub create initiated payment attempt is not configured")
	}

	return s.createInitiatedPaymentAttemptFunc(ctx, arg)
}

func (s stubPaymentAttemptQuerier) GetPaymentAttemptByOrderIDAndIdempotencyKey(ctx context.Context, arg sqlc.GetPaymentAttemptByOrderIDAndIdempotencyKeyParams) (sqlc.PaymentAttempt, error) {
	if s.getPaymentAttemptByOrderIDAndIdempotencyKeyFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub get payment attempt by order id and idempotency key is not configured")
	}

	return s.getPaymentAttemptByOrderIDAndIdempotencyKeyFunc(ctx, arg)
}

func (s stubPaymentAttemptQuerier) MarkPaymentAttemptProcessing(ctx context.Context, arg sqlc.MarkPaymentAttemptProcessingParams) (sqlc.PaymentAttempt, error) {
	if s.markPaymentAttemptProcessingFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub mark payment attempt processing is not configured")
	}

	return s.markPaymentAttemptProcessingFunc(ctx, arg)
}

func (s stubPaymentAttemptQuerier) MarkPaymentAttemptSucceeded(ctx context.Context, arg sqlc.MarkPaymentAttemptSucceededParams) (sqlc.PaymentAttempt, error) {
	if s.markPaymentAttemptSucceededFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub mark payment attempt succeeded is not configured")
	}

	return s.markPaymentAttemptSucceededFunc(ctx, arg)
}

func (s stubPaymentAttemptQuerier) MarkPaymentAttemptFailed(ctx context.Context, arg sqlc.MarkPaymentAttemptFailedParams) (sqlc.PaymentAttempt, error) {
	if s.markPaymentAttemptFailedFunc == nil {
		return sqlc.PaymentAttempt{}, errors.New("stub mark payment attempt failed is not configured")
	}

	return s.markPaymentAttemptFailedFunc(ctx, arg)
}
