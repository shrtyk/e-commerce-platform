package repos

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

func TestConsumerIdempotencyRepositoryCreate(t *testing.T) {
	ctx := context.Background()

	eventID := uuid.New()
	deliveryRequestID := uuid.New()

	t.Run("creates idempotency record", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{
			createConsumerIdempotencyFunc: func(_ context.Context, arg sqlc.CreateConsumerIdempotencyParams) error {
				require.Equal(t, eventID, arg.EventID)
				require.Equal(t, "notification-order-events", arg.ConsumerGroupName)
				require.Equal(t, deliveryRequestID, arg.DeliveryRequestID)

				return nil
			},
		})

		err := repo.Create(ctx, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification-order-events",
			DeliveryRequestID: deliveryRequestID,
		})

		require.NoError(t, err)
	})

	t.Run("returns invalid arg", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{})

		err := repo.Create(ctx, outbound.CreateConsumerIdempotencyInput{})

		require.ErrorIs(t, err, outbound.ErrInvalidConsumerIdempotencyArg)
	})

	t.Run("maps duplicate error", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{
			createConsumerIdempotencyFunc: func(context.Context, sqlc.CreateConsumerIdempotencyParams) error {
				return &pgconn.PgError{Code: "23505"}
			},
		})

		err := repo.Create(ctx, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification-order-events",
			DeliveryRequestID: deliveryRequestID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrConsumerIdempotencyDuplicate)
	})

	t.Run("maps fk error to delivery request not found", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{
			createConsumerIdempotencyFunc: func(context.Context, sqlc.CreateConsumerIdempotencyParams) error {
				return &pgconn.PgError{Code: "23503"}
			},
		})

		err := repo.Create(ctx, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification-order-events",
			DeliveryRequestID: deliveryRequestID,
		})

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})
}

func TestConsumerIdempotencyRepositoryExists(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()

	t.Run("returns exists true", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{
			consumerIdempotencyExistsFunc: func(_ context.Context, arg sqlc.ConsumerIdempotencyExistsParams) (bool, error) {
				require.Equal(t, eventID, arg.EventID)
				require.Equal(t, "notification-order-events", arg.ConsumerGroupName)

				return true, nil
			},
		})

		exists, err := repo.Exists(ctx, eventID, "notification-order-events")

		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("returns invalid arg", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{})

		_, err := repo.Exists(ctx, uuid.Nil, "")

		require.ErrorIs(t, err, outbound.ErrInvalidConsumerIdempotencyArg)
	})

	t.Run("wraps exists error", func(t *testing.T) {
		repo := NewConsumerIdempotencyRepositoryFromQuerier(stubConsumerIdempotencyQuerier{
			consumerIdempotencyExistsFunc: func(context.Context, sqlc.ConsumerIdempotencyExistsParams) (bool, error) {
				return false, errors.New("boom")
			},
		})

		_, err := repo.Exists(ctx, eventID, "notification-order-events")

		require.Error(t, err)
		require.ErrorContains(t, err, "check consumer idempotency exists")
		require.ErrorContains(t, err, "boom")
	})
}

type stubConsumerIdempotencyQuerier struct {
	createConsumerIdempotencyFunc func(context.Context, sqlc.CreateConsumerIdempotencyParams) error
	consumerIdempotencyExistsFunc func(context.Context, sqlc.ConsumerIdempotencyExistsParams) (bool, error)
}

func (s stubConsumerIdempotencyQuerier) CreateConsumerIdempotency(ctx context.Context, arg sqlc.CreateConsumerIdempotencyParams) error {
	if s.createConsumerIdempotencyFunc == nil {
		return errors.New("stub create consumer idempotency is not configured")
	}

	return s.createConsumerIdempotencyFunc(ctx, arg)
}

func (s stubConsumerIdempotencyQuerier) ConsumerIdempotencyExists(ctx context.Context, arg sqlc.ConsumerIdempotencyExistsParams) (bool, error) {
	if s.consumerIdempotencyExistsFunc == nil {
		return false, errors.New("stub consumer idempotency exists is not configured")
	}

	return s.consumerIdempotencyExistsFunc(ctx, arg)
}
