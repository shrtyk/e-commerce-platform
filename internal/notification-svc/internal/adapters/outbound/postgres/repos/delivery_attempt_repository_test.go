package repos

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

func TestDeliveryAttemptRepositoryCreate(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	deliveryRequestID := uuid.New()
	deliveryAttemptID := uuid.New()

	t.Run("creates attempt", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{
			createDeliveryAttemptFunc: func(_ context.Context, arg sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error) {
				require.Equal(t, deliveryRequestID, arg.DeliveryRequestID)
				require.Equal(t, int32(1), arg.AttemptNumber)
				require.Equal(t, "stub-provider", arg.ProviderName)
				require.Equal(t, "provider-msg-id", arg.ProviderMessageID)
				require.False(t, arg.FailureCode.Valid)
				require.False(t, arg.FailureMessage.Valid)
				require.Equal(t, now, arg.AttemptedAt)

				return sqlc.DeliveryAttempt{
					DeliveryAttemptID: deliveryAttemptID,
					DeliveryRequestID: deliveryRequestID,
					AttemptNumber:     1,
					ProviderName:      "stub-provider",
					ProviderMessageID: "provider-msg-id",
					AttemptedAt:       now,
					CreatedAt:         now,
				}, nil
			},
		})

		attempt, err := repo.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-provider",
			ProviderMessageID: "provider-msg-id",
			AttemptedAt:       now,
		})

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryAttempt{
			DeliveryAttemptID: deliveryAttemptID,
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-provider",
			ProviderMessageID: "provider-msg-id",
			AttemptedAt:       now,
		}, attempt)
	})

	t.Run("returns invalid arg on inconsistent failure fields", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{})

		_, err := repo.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-provider",
			FailureCode:       "timeout",
			AttemptedAt:       now,
		})

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryAttemptArg)
	})

	t.Run("returns invalid arg on blank provider fields", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{})

		_, err := repo.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "   ",
			ProviderMessageID: "\t",
			AttemptedAt:       now,
		})

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryAttemptArg)
	})

	t.Run("maps duplicate error", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{
			createDeliveryAttemptFunc: func(context.Context, sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error) {
				return sqlc.DeliveryAttempt{}, &pgconn.PgError{Code: "23505"}
			},
		})

		_, err := repo.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-provider",
			ProviderMessageID: "provider-msg-id",
			AttemptedAt:       now,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryAttemptDuplicate)
	})

	t.Run("maps fk error to delivery request not found", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{
			createDeliveryAttemptFunc: func(context.Context, sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error) {
				return sqlc.DeliveryAttempt{}, &pgconn.PgError{Code: "23503"}
			},
		})

		_, err := repo.Create(ctx, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-provider",
			ProviderMessageID: "provider-msg-id",
			AttemptedAt:       now,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})
}

func TestDeliveryAttemptRepositoryListByDeliveryRequestID(t *testing.T) {
	ctx := context.Background()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	t.Run("lists attempts", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{
			listDeliveryAttemptsByDeliveryRequestIDFunc: func(_ context.Context, id uuid.UUID) ([]sqlc.DeliveryAttempt, error) {
				require.Equal(t, deliveryRequestID, id)

				return []sqlc.DeliveryAttempt{
					{
						DeliveryAttemptID: uuid.New(),
						DeliveryRequestID: deliveryRequestID,
						AttemptNumber:     1,
						ProviderName:      "stub-provider",
						AttemptedAt:       now,
					},
				}, nil
			},
		})

		attempts, err := repo.ListByDeliveryRequestID(ctx, deliveryRequestID)

		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, int32(1), attempts[0].AttemptNumber)
	})

	t.Run("returns invalid arg for nil id", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{})

		_, err := repo.ListByDeliveryRequestID(ctx, uuid.Nil)

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryAttemptArg)
	})

	t.Run("wraps list error", func(t *testing.T) {
		repo := NewDeliveryAttemptRepositoryFromQuerier(stubDeliveryAttemptQuerier{
			listDeliveryAttemptsByDeliveryRequestIDFunc: func(context.Context, uuid.UUID) ([]sqlc.DeliveryAttempt, error) {
				return nil, sql.ErrConnDone
			},
		})

		_, err := repo.ListByDeliveryRequestID(ctx, deliveryRequestID)

		require.Error(t, err)
		require.ErrorIs(t, err, sql.ErrConnDone)
	})
}

type stubDeliveryAttemptQuerier struct {
	createDeliveryAttemptFunc                   func(context.Context, sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error)
	listDeliveryAttemptsByDeliveryRequestIDFunc func(context.Context, uuid.UUID) ([]sqlc.DeliveryAttempt, error)
}

func (s stubDeliveryAttemptQuerier) CreateDeliveryAttempt(ctx context.Context, arg sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error) {
	if s.createDeliveryAttemptFunc == nil {
		return sqlc.DeliveryAttempt{}, errors.New("stub create delivery attempt is not configured")
	}

	return s.createDeliveryAttemptFunc(ctx, arg)
}

func (s stubDeliveryAttemptQuerier) ListDeliveryAttemptsByDeliveryRequestID(ctx context.Context, deliveryRequestID uuid.UUID) ([]sqlc.DeliveryAttempt, error) {
	if s.listDeliveryAttemptsByDeliveryRequestIDFunc == nil {
		return nil, errors.New("stub list delivery attempts by delivery request id is not configured")
	}

	return s.listDeliveryAttemptsByDeliveryRequestIDFunc(ctx, deliveryRequestID)
}
