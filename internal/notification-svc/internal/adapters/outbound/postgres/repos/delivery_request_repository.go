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

type deliveryRequestQuerier interface {
	CreateRequestedDeliveryRequest(ctx context.Context, arg sqlc.CreateRequestedDeliveryRequestParams) (sqlc.DeliveryRequest, error)
	GetDeliveryRequestByID(ctx context.Context, deliveryRequestID uuid.UUID) (sqlc.DeliveryRequest, error)
	GetDeliveryRequestByIdempotencyKey(ctx context.Context, idempotencyKey string) (sqlc.DeliveryRequest, error)
	MarkDeliveryRequestSent(ctx context.Context, arg sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error)
	MarkDeliveryRequestFailed(ctx context.Context, arg sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error)
}

type DeliveryRequestRepository struct {
	queries deliveryRequestQuerier
}

func NewDeliveryRequestRepository(db *sql.DB) *DeliveryRequestRepository {
	return NewDeliveryRequestRepositoryFromQuerier(sqlc.New(db))
}

func NewDeliveryRequestRepositoryFromTx(tx *sql.Tx) *DeliveryRequestRepository {
	return NewDeliveryRequestRepositoryFromQuerier(sqlc.New(tx))
}

func NewDeliveryRequestRepositoryFromQuerier(queries deliveryRequestQuerier) *DeliveryRequestRepository {
	return &DeliveryRequestRepository{queries: queries}
}

func (r *DeliveryRequestRepository) CreateRequested(
	ctx context.Context,
	input outbound.CreateDeliveryRequestInput,
) (domain.DeliveryRequest, error) {
	sourceEventName := strings.TrimSpace(input.SourceEventName)
	channel := strings.TrimSpace(input.Channel)
	recipient := strings.TrimSpace(input.Recipient)
	templateKey := strings.TrimSpace(input.TemplateKey)
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)

	if input.SourceEventID == uuid.Nil || sourceEventName == "" || channel == "" || recipient == "" || templateKey == "" || idempotencyKey == "" {
		return domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg
	}

	deliveryRequest, err := r.queries.CreateRequestedDeliveryRequest(ctx, sqlc.CreateRequestedDeliveryRequestParams{
		SourceEventID:   input.SourceEventID,
		SourceEventName: sourceEventName,
		Channel:         channel,
		Recipient:       recipient,
		TemplateKey:     templateKey,
		Status:          sqlc.DeliveryStatusRequested,
		IdempotencyKey:  idempotencyKey,
	})
	if err != nil {
		return domain.DeliveryRequest{}, fmt.Errorf("create requested delivery request: %w", mapDeliveryRequestErr(err))
	}

	return toDomainDeliveryRequest(deliveryRequest), nil
}

func (r *DeliveryRequestRepository) GetByID(ctx context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error) {
	if deliveryRequestID == uuid.Nil {
		return domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg
	}

	deliveryRequest, err := r.queries.GetDeliveryRequestByID(ctx, deliveryRequestID)
	if err != nil {
		return domain.DeliveryRequest{}, fmt.Errorf("get delivery request by id: %w", mapDeliveryRequestErr(err))
	}

	return toDomainDeliveryRequest(deliveryRequest), nil
}

func (r *DeliveryRequestRepository) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (domain.DeliveryRequest, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)

	if idempotencyKey == "" {
		return domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg
	}

	deliveryRequest, err := r.queries.GetDeliveryRequestByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return domain.DeliveryRequest{}, fmt.Errorf("get delivery request by idempotency key: %w", mapDeliveryRequestErr(err))
	}

	return toDomainDeliveryRequest(deliveryRequest), nil
}

func (r *DeliveryRequestRepository) MarkSent(ctx context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error) {
	if deliveryRequestID == uuid.Nil {
		return domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg
	}

	deliveryRequest, err := r.queries.MarkDeliveryRequestSent(ctx, sqlc.MarkDeliveryRequestSentParams{
		Status:            sqlc.DeliveryStatusSent,
		DeliveryRequestID: deliveryRequestID,
	})
	if err != nil {
		return domain.DeliveryRequest{}, fmt.Errorf("mark delivery request sent: %w", mapDeliveryRequestErr(err))
	}

	return toDomainDeliveryRequest(deliveryRequest), nil
}

func (r *DeliveryRequestRepository) MarkFailed(
	ctx context.Context,
	deliveryRequestID uuid.UUID,
	failureCode string,
	failureMessage string,
) (domain.DeliveryRequest, error) {
	failureCode = strings.TrimSpace(failureCode)
	failureMessage = strings.TrimSpace(failureMessage)

	if deliveryRequestID == uuid.Nil || failureCode == "" || failureMessage == "" {
		return domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg
	}

	deliveryRequest, err := r.queries.MarkDeliveryRequestFailed(ctx, sqlc.MarkDeliveryRequestFailedParams{
		Status:            sqlc.DeliveryStatusFailed,
		LastErrorCode:     sql.NullString{String: failureCode, Valid: true},
		LastErrorMessage:  sql.NullString{String: failureMessage, Valid: true},
		DeliveryRequestID: deliveryRequestID,
	})
	if err != nil {
		return domain.DeliveryRequest{}, fmt.Errorf("mark delivery request failed: %w", mapDeliveryRequestErr(err))
	}

	return toDomainDeliveryRequest(deliveryRequest), nil
}

func toDomainDeliveryRequest(deliveryRequest sqlc.DeliveryRequest) domain.DeliveryRequest {
	return domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequest.DeliveryRequestID,
		SourceEventID:     deliveryRequest.SourceEventID,
		SourceEventName:   deliveryRequest.SourceEventName,
		Channel:           deliveryRequest.Channel,
		Recipient:         deliveryRequest.Recipient,
		TemplateKey:       deliveryRequest.TemplateKey,
		Status:            toDomainDeliveryStatus(deliveryRequest.Status),
		IdempotencyKey:    deliveryRequest.IdempotencyKey,
		LastErrorCode:     deliveryRequest.LastErrorCode.String,
		LastErrorMessage:  deliveryRequest.LastErrorMessage.String,
		CreatedAt:         deliveryRequest.CreatedAt,
		UpdatedAt:         deliveryRequest.UpdatedAt,
	}
}

func toDomainDeliveryStatus(status sqlc.DeliveryStatus) domain.DeliveryStatus {
	switch status {
	case sqlc.DeliveryStatusRequested:
		return domain.DeliveryStatusRequested
	case sqlc.DeliveryStatusSent:
		return domain.DeliveryStatusSent
	case sqlc.DeliveryStatusFailed:
		return domain.DeliveryStatusFailed
	default:
		return domain.DeliveryStatusUnknown
	}
}

func mapDeliveryRequestErr(err error) error {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if ok {
		switch pgErr.Code {
		case "23505":
			return outbound.ErrDeliveryRequestDuplicate
		}
	}

	if errors.Is(err, sql.ErrNoRows) {
		return outbound.ErrDeliveryRequestNotFound
	}

	return err
}

var _ outbound.DeliveryRequestRepository = (*DeliveryRequestRepository)(nil)
