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

func TestDeliveryRequestRepositoryCreateRequested(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()

	t.Run("creates requested delivery request", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			createRequestedDeliveryRequestFunc: func(_ context.Context, arg sqlc.CreateRequestedDeliveryRequestParams) (sqlc.DeliveryRequest, error) {
				require.Equal(t, sourceEventID, arg.SourceEventID)
				require.Equal(t, "corr-order-confirmed", arg.CorrelationID)
				require.Equal(t, "order.confirmed", arg.SourceEventName)
				require.Equal(t, "email", arg.Channel)
				require.Equal(t, "user@example.com", arg.Recipient)
				require.Equal(t, "order-confirmed", arg.TemplateKey)
				require.Equal(t, sqlc.DeliveryStatusRequested, arg.Status)
				require.Equal(t, "idem-1", arg.IdempotencyKey)

				return sqlc.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					SourceEventID:     sourceEventID,
					CorrelationID:     "corr-order-confirmed",
					SourceEventName:   "order.confirmed",
					Channel:           "email",
					Recipient:         "user@example.com",
					TemplateKey:       "order-confirmed",
					Status:            sqlc.DeliveryStatusRequested,
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		result, err := repo.CreateRequested(ctx, outbound.CreateDeliveryRequestInput{
			SourceEventID:   sourceEventID,
			CorrelationID:   "corr-order-confirmed",
			SourceEventName: "order.confirmed",
			Channel:         "email",
			Recipient:       "user@example.com",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  "idem-1",
		})

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			SourceEventID:     sourceEventID,
			CorrelationID:     "corr-order-confirmed",
			SourceEventName:   "order.confirmed",
			Channel:           "email",
			Recipient:         "user@example.com",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
			IdempotencyKey:    "idem-1",
			CreatedAt:         now,
			UpdatedAt:         now,
		}, result)
	})

	t.Run("returns invalid arg", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{})

		_, err := repo.CreateRequested(ctx, outbound.CreateDeliveryRequestInput{})

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryRequestArg)
	})

	t.Run("maps duplicate error", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			createRequestedDeliveryRequestFunc: func(context.Context, sqlc.CreateRequestedDeliveryRequestParams) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, &pgconn.PgError{Code: "23505"}
			},
		})

		_, err := repo.CreateRequested(ctx, outbound.CreateDeliveryRequestInput{
			SourceEventID:   sourceEventID,
			CorrelationID:   "corr-order-confirmed",
			SourceEventName: "order.confirmed",
			Channel:         "email",
			Recipient:       "user@example.com",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  "idem-1",
		})

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestDuplicate)
	})
}

func TestDeliveryRequestRepositoryGetByID(t *testing.T) {
	ctx := context.Background()
	deliveryRequestID := uuid.New()

	t.Run("maps no rows to not found", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			getDeliveryRequestByIDFunc: func(context.Context, uuid.UUID) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrNoRows
			},
		})

		_, err := repo.GetByID(ctx, deliveryRequestID)

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})
}

func TestDeliveryRequestRepositoryGetByIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()

	t.Run("returns delivery request", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			getByIdempotencyKeyFunc: func(_ context.Context, key string) (sqlc.DeliveryRequest, error) {
				require.Equal(t, "idem-1", key)

				return sqlc.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					SourceEventID:     sourceEventID,
					CorrelationID:     "corr-order-confirmed",
					SourceEventName:   "order.confirmed",
					Channel:           "email",
					Recipient:         "user@example.com",
					TemplateKey:       "order-confirmed",
					Status:            sqlc.DeliveryStatusRequested,
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		result, err := repo.GetByIdempotencyKey(ctx, "idem-1")

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			SourceEventID:     sourceEventID,
			CorrelationID:     "corr-order-confirmed",
			SourceEventName:   "order.confirmed",
			Channel:           "email",
			Recipient:         "user@example.com",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
			IdempotencyKey:    "idem-1",
			CreatedAt:         now,
			UpdatedAt:         now,
		}, result)
	})

	t.Run("maps no rows to not found", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			getByIdempotencyKeyFunc: func(context.Context, string) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrNoRows
			},
		})

		_, err := repo.GetByIdempotencyKey(ctx, "idem-1")

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})

	t.Run("wraps query error", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			getByIdempotencyKeyFunc: func(context.Context, string) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrConnDone
			},
		})

		_, err := repo.GetByIdempotencyKey(ctx, "idem-1")

		require.Error(t, err)
		require.ErrorIs(t, err, sql.ErrConnDone)
	})

	t.Run("returns invalid arg on blank key", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{})

		_, err := repo.GetByIdempotencyKey(ctx, "   ")

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryRequestArg)
	})
}

func TestDeliveryRequestRepositoryMarkSent(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()

	t.Run("marks request sent", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			markDeliveryRequestSentFunc: func(_ context.Context, arg sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error) {
				require.Equal(t, sqlc.DeliveryStatusSent, arg.Status)
				require.Equal(t, deliveryRequestID, arg.DeliveryRequestID)

				return sqlc.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					SourceEventID:     sourceEventID,
					CorrelationID:     "corr-order-confirmed",
					SourceEventName:   "order.confirmed",
					Channel:           "email",
					Recipient:         "user@example.com",
					TemplateKey:       "order-confirmed",
					Status:            sqlc.DeliveryStatusSent,
					IdempotencyKey:    "idem-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		result, err := repo.MarkSent(ctx, deliveryRequestID)

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			SourceEventID:     sourceEventID,
			CorrelationID:     "corr-order-confirmed",
			SourceEventName:   "order.confirmed",
			Channel:           "email",
			Recipient:         "user@example.com",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusSent,
			IdempotencyKey:    "idem-1",
			CreatedAt:         now,
			UpdatedAt:         now,
		}, result)
	})

	t.Run("maps no rows to not found", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			markDeliveryRequestSentFunc: func(context.Context, sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrNoRows
			},
		})

		_, err := repo.MarkSent(ctx, deliveryRequestID)

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})

	t.Run("wraps query error", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			markDeliveryRequestSentFunc: func(context.Context, sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrConnDone
			},
		})

		_, err := repo.MarkSent(ctx, deliveryRequestID)

		require.Error(t, err)
		require.ErrorIs(t, err, sql.ErrConnDone)
	})
}

func TestDeliveryRequestRepositoryMarkFailed(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()

		t.Run("marks request failed", func(t *testing.T) {
			repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
				markDeliveryRequestFailedFunc: func(_ context.Context, arg sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error) {
				require.Equal(t, sqlc.DeliveryStatusFailed, arg.Status)
				require.Equal(t, deliveryRequestID, arg.DeliveryRequestID)
				require.Equal(t, sql.NullString{String: "timeout", Valid: true}, arg.LastErrorCode)
				require.Equal(t, sql.NullString{String: "provider timeout", Valid: true}, arg.LastErrorMessage)

					return sqlc.DeliveryRequest{
						DeliveryRequestID: deliveryRequestID,
						SourceEventID:     sourceEventID,
						CorrelationID:     "corr-order-confirmed",
						SourceEventName:   "order.confirmed",
						Channel:           "email",
						Recipient:         "user@example.com",
					TemplateKey:       "order-confirmed",
					Status:            sqlc.DeliveryStatusFailed,
					IdempotencyKey:    "idem-1",
					LastErrorCode:     sql.NullString{String: "timeout", Valid: true},
					LastErrorMessage:  sql.NullString{String: "provider timeout", Valid: true},
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil
			},
		})

		result, err := repo.MarkFailed(ctx, deliveryRequestID, "timeout", "provider timeout")

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			SourceEventID:     sourceEventID,
			CorrelationID:     "corr-order-confirmed",
			SourceEventName:   "order.confirmed",
			Channel:           "email",
			Recipient:         "user@example.com",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusFailed,
			IdempotencyKey:    "idem-1",
			LastErrorCode:     "timeout",
			LastErrorMessage:  "provider timeout",
			CreatedAt:         now,
			UpdatedAt:         now,
		}, result)
	})

	t.Run("maps no rows to not found", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			markDeliveryRequestFailedFunc: func(context.Context, sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrNoRows
			},
		})

		_, err := repo.MarkFailed(ctx, deliveryRequestID, "timeout", "provider timeout")

		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrDeliveryRequestNotFound)
	})

	t.Run("wraps query error", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{
			markDeliveryRequestFailedFunc: func(context.Context, sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error) {
				return sqlc.DeliveryRequest{}, sql.ErrConnDone
			},
		})

		_, err := repo.MarkFailed(ctx, deliveryRequestID, "timeout", "provider timeout")

		require.Error(t, err)
		require.ErrorIs(t, err, sql.ErrConnDone)
	})

	t.Run("returns invalid arg when failure details empty", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{})

		_, err := repo.MarkFailed(ctx, deliveryRequestID, "", "")

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryRequestArg)
	})

	t.Run("returns invalid arg when failure details are blank", func(t *testing.T) {
		repo := NewDeliveryRequestRepositoryFromQuerier(stubDeliveryRequestQuerier{})

		_, err := repo.MarkFailed(ctx, deliveryRequestID, "   ", "\t")

		require.ErrorIs(t, err, outbound.ErrInvalidDeliveryRequestArg)
	})
}

type stubDeliveryRequestQuerier struct {
	createRequestedDeliveryRequestFunc func(context.Context, sqlc.CreateRequestedDeliveryRequestParams) (sqlc.DeliveryRequest, error)
	getDeliveryRequestByIDFunc         func(context.Context, uuid.UUID) (sqlc.DeliveryRequest, error)
	getByIdempotencyKeyFunc            func(context.Context, string) (sqlc.DeliveryRequest, error)
	markDeliveryRequestSentFunc        func(context.Context, sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error)
	markDeliveryRequestFailedFunc      func(context.Context, sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error)
}

func (s stubDeliveryRequestQuerier) CreateRequestedDeliveryRequest(ctx context.Context, arg sqlc.CreateRequestedDeliveryRequestParams) (sqlc.DeliveryRequest, error) {
	if s.createRequestedDeliveryRequestFunc == nil {
		return sqlc.DeliveryRequest{}, errors.New("stub create requested delivery request is not configured")
	}

	return s.createRequestedDeliveryRequestFunc(ctx, arg)
}

func (s stubDeliveryRequestQuerier) GetDeliveryRequestByID(ctx context.Context, deliveryRequestID uuid.UUID) (sqlc.DeliveryRequest, error) {
	if s.getDeliveryRequestByIDFunc == nil {
		return sqlc.DeliveryRequest{}, errors.New("stub get delivery request by id is not configured")
	}

	return s.getDeliveryRequestByIDFunc(ctx, deliveryRequestID)
}

func (s stubDeliveryRequestQuerier) GetDeliveryRequestByIdempotencyKey(ctx context.Context, idempotencyKey string) (sqlc.DeliveryRequest, error) {
	if s.getByIdempotencyKeyFunc == nil {
		return sqlc.DeliveryRequest{}, errors.New("stub get delivery request by idempotency key is not configured")
	}

	return s.getByIdempotencyKeyFunc(ctx, idempotencyKey)
}

func (s stubDeliveryRequestQuerier) MarkDeliveryRequestSent(ctx context.Context, arg sqlc.MarkDeliveryRequestSentParams) (sqlc.DeliveryRequest, error) {
	if s.markDeliveryRequestSentFunc == nil {
		return sqlc.DeliveryRequest{}, errors.New("stub mark delivery request sent is not configured")
	}

	return s.markDeliveryRequestSentFunc(ctx, arg)
}

func (s stubDeliveryRequestQuerier) MarkDeliveryRequestFailed(ctx context.Context, arg sqlc.MarkDeliveryRequestFailedParams) (sqlc.DeliveryRequest, error) {
	if s.markDeliveryRequestFailedFunc == nil {
		return sqlc.DeliveryRequest{}, errors.New("stub mark delivery request failed is not configured")
	}

	return s.markDeliveryRequestFailedFunc(ctx, arg)
}
