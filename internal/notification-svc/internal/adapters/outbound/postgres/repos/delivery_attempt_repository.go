package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

type deliveryAttemptQuerier interface {
	CreateDeliveryAttempt(ctx context.Context, arg sqlc.CreateDeliveryAttemptParams) (sqlc.DeliveryAttempt, error)
	ListDeliveryAttemptsByDeliveryRequestID(ctx context.Context, deliveryRequestID uuid.UUID) ([]sqlc.DeliveryAttempt, error)
}

type DeliveryAttemptRepository struct {
	queries deliveryAttemptQuerier
}

func NewDeliveryAttemptRepository(db *sql.DB) *DeliveryAttemptRepository {
	return NewDeliveryAttemptRepositoryFromQuerier(sqlc.New(db))
}

func NewDeliveryAttemptRepositoryFromTx(tx *sql.Tx) *DeliveryAttemptRepository {
	return NewDeliveryAttemptRepositoryFromQuerier(sqlc.New(tx))
}

func NewDeliveryAttemptRepositoryFromQuerier(queries deliveryAttemptQuerier) *DeliveryAttemptRepository {
	return &DeliveryAttemptRepository{queries: queries}
}

func (r *DeliveryAttemptRepository) Create(ctx context.Context, input outbound.CreateDeliveryAttemptInput) (domain.DeliveryAttempt, error) {
	providerName := strings.TrimSpace(input.ProviderName)
	providerMessageID := strings.TrimSpace(input.ProviderMessageID)
	failureCode := strings.TrimSpace(input.FailureCode)
	failureMessage := strings.TrimSpace(input.FailureMessage)

	if input.DeliveryRequestID == uuid.Nil || input.AttemptNumber <= 0 || providerName == "" || providerMessageID == "" || input.AttemptedAt.IsZero() {
		return domain.DeliveryAttempt{}, outbound.ErrInvalidDeliveryAttemptArg
	}
	if (failureCode == "") != (failureMessage == "") {
		return domain.DeliveryAttempt{}, outbound.ErrInvalidDeliveryAttemptArg
	}

	deliveryAttempt, err := r.queries.CreateDeliveryAttempt(ctx, sqlc.CreateDeliveryAttemptParams{
		DeliveryRequestID: input.DeliveryRequestID,
		AttemptNumber:     input.AttemptNumber,
		ProviderName:      providerName,
		ProviderMessageID: providerMessageID,
		FailureCode:       sql.NullString{String: failureCode, Valid: failureCode != ""},
		FailureMessage:    sql.NullString{String: failureMessage, Valid: failureMessage != ""},
		AttemptedAt:       input.AttemptedAt,
	})
	if err != nil {
		return domain.DeliveryAttempt{}, fmt.Errorf("create delivery attempt: %w", mapDeliveryAttemptErr(err))
	}

	return toDomainDeliveryAttempt(deliveryAttempt), nil
}

func (r *DeliveryAttemptRepository) ListByDeliveryRequestID(ctx context.Context, deliveryRequestID uuid.UUID) ([]domain.DeliveryAttempt, error) {
	if deliveryRequestID == uuid.Nil {
		return nil, outbound.ErrInvalidDeliveryAttemptArg
	}

	attempts, err := r.queries.ListDeliveryAttemptsByDeliveryRequestID(ctx, deliveryRequestID)
	if err != nil {
		return nil, fmt.Errorf("list delivery attempts by delivery request id: %w", mapDeliveryAttemptErr(err))
	}

	result := make([]domain.DeliveryAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		result = append(result, toDomainDeliveryAttempt(attempt))
	}

	return result, nil
}

func toDomainDeliveryAttempt(attempt sqlc.DeliveryAttempt) domain.DeliveryAttempt {
	return domain.DeliveryAttempt{
		DeliveryAttemptID: attempt.DeliveryAttemptID,
		DeliveryRequestID: attempt.DeliveryRequestID,
		AttemptNumber:     attempt.AttemptNumber,
		ProviderName:      attempt.ProviderName,
		ProviderMessageID: attempt.ProviderMessageID,
		FailureCode:       attempt.FailureCode.String,
		FailureMessage:    attempt.FailureMessage.String,
		AttemptedAt:       attempt.AttemptedAt,
	}
}

func mapDeliveryAttemptErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return outbound.ErrDeliveryAttemptDuplicate
		case "23503":
			return outbound.ErrDeliveryRequestNotFound
		}
	}

	return err
}

var _ outbound.DeliveryAttemptRepository = (*DeliveryAttemptRepository)(nil)
