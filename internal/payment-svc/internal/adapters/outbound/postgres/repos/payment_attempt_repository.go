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
	GetPaymentAttemptByOrderIDAndIdempotencyKey(ctx context.Context, arg sqlc.GetPaymentAttemptByOrderIDAndIdempotencyKeyParams) (sqlc.PaymentAttempt, error)
	MarkPaymentAttemptProcessing(ctx context.Context, arg sqlc.MarkPaymentAttemptProcessingParams) (sqlc.PaymentAttempt, error)
	MarkPaymentAttemptSucceeded(ctx context.Context, arg sqlc.MarkPaymentAttemptSucceededParams) (sqlc.PaymentAttempt, error)
	MarkPaymentAttemptFailed(ctx context.Context, arg sqlc.MarkPaymentAttemptFailedParams) (sqlc.PaymentAttempt, error)
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

	return toDomainPaymentAttempt(attempt), nil
}

func (r *PaymentAttemptRepository) GetByOrderIDAndIdempotencyKey(
	ctx context.Context,
	orderID uuid.UUID,
	idempotencyKey string,
) (domain.PaymentAttempt, error) {
	if orderID == uuid.Nil || idempotencyKey == "" {
		return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
	}

	attempt, err := r.queries.GetPaymentAttemptByOrderIDAndIdempotencyKey(ctx, sqlc.GetPaymentAttemptByOrderIDAndIdempotencyKeyParams{
		OrderID:        orderID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return domain.PaymentAttempt{}, fmt.Errorf("get payment attempt by order id and idempotency key: %w", mapPaymentAttemptErr(err))
	}

	return toDomainPaymentAttempt(attempt), nil
}

func (r *PaymentAttemptRepository) MarkProcessing(
	ctx context.Context,
	paymentAttemptID uuid.UUID,
) (domain.PaymentAttempt, error) {
	if paymentAttemptID == uuid.Nil {
		return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
	}

	attempt, err := r.queries.MarkPaymentAttemptProcessing(ctx, sqlc.MarkPaymentAttemptProcessingParams{
		Status:           sqlc.PaymentStatusProcessing,
		PaymentAttemptID: paymentAttemptID,
	})
	if err != nil {
		return domain.PaymentAttempt{}, fmt.Errorf("mark payment attempt processing: %w", mapPaymentAttemptErr(err))
	}

	return toDomainPaymentAttempt(attempt), nil
}

func (r *PaymentAttemptRepository) MarkSucceeded(
	ctx context.Context,
	paymentAttemptID uuid.UUID,
	providerReference string,
) (domain.PaymentAttempt, error) {
	if paymentAttemptID == uuid.Nil || providerReference == "" {
		return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
	}

	attempt, err := r.queries.MarkPaymentAttemptSucceeded(ctx, sqlc.MarkPaymentAttemptSucceededParams{
		Status:            sqlc.PaymentStatusSucceeded,
		ProviderReference: providerReference,
		PaymentAttemptID:  paymentAttemptID,
	})
	if err != nil {
		return domain.PaymentAttempt{}, fmt.Errorf("mark payment attempt succeeded: %w", mapPaymentAttemptErr(err))
	}

	return toDomainPaymentAttempt(attempt), nil
}

func (r *PaymentAttemptRepository) MarkFailed(
	ctx context.Context,
	paymentAttemptID uuid.UUID,
	failureCode string,
	failureMessage string,
) (domain.PaymentAttempt, error) {
	if paymentAttemptID == uuid.Nil || failureCode == "" || failureMessage == "" {
		return domain.PaymentAttempt{}, outbound.ErrInvalidPaymentAttemptArg
	}

	attempt, err := r.queries.MarkPaymentAttemptFailed(ctx, sqlc.MarkPaymentAttemptFailedParams{
		Status:           sqlc.PaymentStatusFailed,
		FailureCode:      sql.NullString{String: failureCode, Valid: true},
		FailureMessage:   sql.NullString{String: failureMessage, Valid: true},
		PaymentAttemptID: paymentAttemptID,
	})
	if err != nil {
		return domain.PaymentAttempt{}, fmt.Errorf("mark payment attempt failed: %w", mapPaymentAttemptErr(err))
	}

	return toDomainPaymentAttempt(attempt), nil
}

func toDomainPaymentAttempt(attempt sqlc.PaymentAttempt) domain.PaymentAttempt {
	return domain.PaymentAttempt{
		PaymentAttemptID:  attempt.PaymentAttemptID,
		OrderID:           attempt.OrderID,
		Status:            toDomainPaymentStatus(attempt.Status),
		Amount:            attempt.Amount,
		Currency:          attempt.Currency,
		ProviderName:      attempt.ProviderName,
		ProviderReference: attempt.ProviderReference,
		IdempotencyKey:    attempt.IdempotencyKey,
		FailureCode:       attempt.FailureCode.String,
		FailureMessage:    attempt.FailureMessage.String,
		CreatedAt:         attempt.CreatedAt,
		UpdatedAt:         attempt.UpdatedAt,
	}
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

	if errors.Is(err, sql.ErrNoRows) {
		return outbound.ErrPaymentAttemptNotFound
	}

	return err
}

var _ outbound.PaymentAttemptRepository = (*PaymentAttemptRepository)(nil)
