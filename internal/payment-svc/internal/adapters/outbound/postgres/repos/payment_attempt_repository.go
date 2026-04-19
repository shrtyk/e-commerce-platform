package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/ports/outbound"
)

type paymentAttemptQuerier interface {
	CreateInitiatedPaymentAttempt(ctx context.Context, arg sqlc.CreateInitiatedPaymentAttemptParams) (sqlc.PaymentAttempt, error)
}

type PaymentAttemptRepository struct {
	queries paymentAttemptQuerier
}

func NewPaymentAttemptRepository(db *sql.DB) *PaymentAttemptRepository {
	return NewPaymentAttemptRepositoryFromQuerier(sqlc.New(db))
}

func NewPaymentAttemptRepositoryFromTx(tx *sql.Tx) *PaymentAttemptRepository {
	return NewPaymentAttemptRepositoryFromQuerier(sqlc.New(tx))
}

func NewPaymentAttemptRepositoryFromQuerier(queries paymentAttemptQuerier) *PaymentAttemptRepository {
	return &PaymentAttemptRepository{queries: queries}
}

func (r *PaymentAttemptRepository) CreateInitiated(
	ctx context.Context,
	input outbound.CreatePaymentAttemptInput,
) (domain.PaymentAttempt, error) {
	if input.OrderID == uuid.Nil || input.Amount <= 0 || input.Currency == "" || input.ProviderName == "" || input.IdempotencyKey == "" {
		return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
	}

	attempt, err := r.queries.CreateInitiatedPaymentAttempt(ctx, sqlc.CreateInitiatedPaymentAttemptParams{
		OrderID:        input.OrderID,
		Status:         sqlc.PaymentStatusInitiated,
		Amount:         input.Amount,
		Currency:       input.Currency,
		ProviderName:   input.ProviderName,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return domain.PaymentAttempt{}, fmt.Errorf("create initiated payment attempt: %w", mapPaymentAttemptErr(err))
	}

	return domain.PaymentAttempt{
		PaymentAttemptID:  attempt.PaymentAttemptID,
		OrderID:           attempt.OrderID,
		Status:            toDomainPaymentStatus(attempt.Status),
		Amount:            attempt.Amount,
		Currency:          attempt.Currency,
		ProviderName:      attempt.ProviderName,
		ProviderReference: attempt.ProviderReference,
		IdempotencyKey:    attempt.IdempotencyKey,
		CreatedAt:         attempt.CreatedAt,
		UpdatedAt:         attempt.UpdatedAt,
	}, nil
}

func toDomainPaymentStatus(status sqlc.PaymentStatus) domain.PaymentStatus {
	switch status {
	case sqlc.PaymentStatusInitiated:
		return domain.PaymentStatusInitiated
	case sqlc.PaymentStatusProcessing:
		return domain.PaymentStatusProcessing
	case sqlc.PaymentStatusSucceeded:
		return domain.PaymentStatusSucceeded
	case sqlc.PaymentStatusFailed:
		return domain.PaymentStatusFailed
	default:
		return domain.PaymentStatusUnknown
	}
}

func mapPaymentAttemptErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return outbound.ErrPaymentAttemptDuplicate
		}
	}

	return err
}

var _ outbound.PaymentAttemptRepository = (*PaymentAttemptRepository)(nil)
