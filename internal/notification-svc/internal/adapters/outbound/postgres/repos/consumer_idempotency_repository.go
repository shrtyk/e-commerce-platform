package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

type consumerIdempotencyQuerier interface {
	CreateConsumerIdempotency(ctx context.Context, arg sqlc.CreateConsumerIdempotencyParams) error
	ConsumerIdempotencyExists(ctx context.Context, arg sqlc.ConsumerIdempotencyExistsParams) (bool, error)
}

type ConsumerIdempotencyRepository struct {
	queries consumerIdempotencyQuerier
}

func NewConsumerIdempotencyRepository(db *sql.DB) *ConsumerIdempotencyRepository {
	return NewConsumerIdempotencyRepositoryFromQuerier(sqlc.New(db))
}

func NewConsumerIdempotencyRepositoryFromTx(tx *sql.Tx) *ConsumerIdempotencyRepository {
	return NewConsumerIdempotencyRepositoryFromQuerier(sqlc.New(tx))
}

func NewConsumerIdempotencyRepositoryFromQuerier(queries consumerIdempotencyQuerier) *ConsumerIdempotencyRepository {
	return &ConsumerIdempotencyRepository{queries: queries}
}

func (r *ConsumerIdempotencyRepository) Create(ctx context.Context, input outbound.CreateConsumerIdempotencyInput) error {
	if input.EventID == uuid.Nil || input.ConsumerGroupName == "" || input.DeliveryRequestID == uuid.Nil {
		return outbound.ErrInvalidConsumerIdempotencyArg
	}

	err := r.queries.CreateConsumerIdempotency(ctx, sqlc.CreateConsumerIdempotencyParams{
		EventID:           input.EventID,
		ConsumerGroupName: input.ConsumerGroupName,
		DeliveryRequestID: input.DeliveryRequestID,
	})
	if err != nil {
		return fmt.Errorf("create consumer idempotency: %w", mapConsumerIdempotencyErr(err))
	}

	return nil
}

func (r *ConsumerIdempotencyRepository) Exists(ctx context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error) {
	if eventID == uuid.Nil || consumerGroupName == "" {
		return false, outbound.ErrInvalidConsumerIdempotencyArg
	}

	exists, err := r.queries.ConsumerIdempotencyExists(ctx, sqlc.ConsumerIdempotencyExistsParams{
		EventID:           eventID,
		ConsumerGroupName: consumerGroupName,
	})
	if err != nil {
		return false, fmt.Errorf("check consumer idempotency exists: %w", err)
	}

	return exists, nil
}

func mapConsumerIdempotencyErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return outbound.ErrConsumerIdempotencyDuplicate
		case "23503":
			return outbound.ErrDeliveryRequestNotFound
		}
	}

	return err
}

var _ outbound.ConsumerIdempotencyRepository = (*ConsumerIdempotencyRepository)(nil)
