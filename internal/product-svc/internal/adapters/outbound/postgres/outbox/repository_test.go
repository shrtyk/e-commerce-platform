package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/stretchr/testify/require"
)

type stubQuerier struct {
	appendFunc               func(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	claimPendingFunc         func(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	claimStaleInProgressFunc func(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	markPublishedFunc        func(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
	markRetryableFailureFunc func(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error)
	markDeadFunc             func(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error)
}

func (s stubQuerier) AppendOutboxRecord(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
	if s.appendFunc == nil {
		return sqlc.OutboxRecord{}, fmt.Errorf("unexpected AppendOutboxRecord call")
	}

	return s.appendFunc(ctx, arg)
}

func (s stubQuerier) ClaimPendingOutboxRecords(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.claimPendingFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimPendingOutboxRecords call")
	}

	return s.claimPendingFunc(ctx, arg)
}

func (s stubQuerier) ClaimStaleInProgressOutboxRecords(ctx context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
	if s.claimStaleInProgressFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimStaleInProgressOutboxRecords call")
	}

	return s.claimStaleInProgressFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
	if s.markPublishedFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordPublished call")
	}

	return s.markPublishedFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordRetryableFailure(ctx context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error) {
	if s.markRetryableFailureFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordRetryableFailure call")
	}

	return s.markRetryableFailureFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordDead(ctx context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error) {
	if s.markDeadFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordDead call")
	}

	return s.markDeadFunc(ctx, arg)
}

func TestRepositoryAppend(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	eventID := uuid.New()

	tests := []struct {
		name   string
		record commonoutbox.Record
		setup  func(*stubQuerier)
		errIs  error
	}{
		{
			name: "success",
			record: commonoutbox.Record{
				EventID:       eventID.String(),
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events",
				Key:           []byte("product-1"),
				Payload:       []byte("payload"),
				Headers:       map[string]string{"eventName": "catalog.product.created"},
				Status:        commonoutbox.StatusPending,
			},
			setup: func(q *stubQuerier) {
				q.appendFunc = func(_ context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
					require.Equal(t, eventID, arg.EventID)

					var headers map[string]string
					err := json.Unmarshal(arg.Headers, &headers)
					require.NoError(t, err)
					require.Equal(t, "catalog.product.created", headers["eventName"])

					return sqlc.OutboxRecord{
						ID:            uuid.New(),
						EventID:       arg.EventID,
						EventName:     arg.EventName,
						AggregateType: arg.AggregateType,
						AggregateID:   arg.AggregateID,
						Topic:         arg.Topic,
						Key:           arg.Key,
						Payload:       arg.Payload,
						Headers:       arg.Headers,
						Status:        commonoutbox.StatusPending,
						NextAttemptAt: now,
						MaxAttempts:   20,
						CreatedAt:     now,
						UpdatedAt:     now,
					}, nil
				}
			},
		},
		{
			name:   "invalid record",
			record: commonoutbox.Record{},
			errIs:  commonoutbox.ErrInvalidRecord,
		},
		{
			name: "idempotency conflict",
			record: commonoutbox.Record{
				EventID:       eventID.String(),
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events",
				Payload:       []byte("payload"),
				Status:        commonoutbox.StatusPending,
			},
			setup: func(q *stubQuerier) {
				q.appendFunc = func(_ context.Context, _ sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
					return sqlc.OutboxRecord{}, &pgconn.PgError{Code: "23505"}
				}
			},
			errIs: commonoutbox.ErrIdempotencyConflict,
		},
		{
			name: "invalid event id",
			record: commonoutbox.Record{
				EventID:       "not-uuid",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events",
				Payload:       []byte("payload"),
				Status:        commonoutbox.StatusPending,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := &stubQuerier{}
			if tt.setup != nil {
				tt.setup(queries)
			}

			repo := NewRepositoryFromQuerier(queries)
			created, err := repo.Append(context.Background(), tt.record)
			if tt.errIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errIs)
				require.Zero(t, created)
				return
			}

			if tt.name == "invalid event id" {
				require.Error(t, err)
				require.ErrorContains(t, err, "parse event id")
				require.Zero(t, created)
				return
			}

			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, created.ID)
			require.Equal(t, commonoutbox.StatusPending, created.Status)
			require.Equal(t, 20, created.MaxAttempts)
		})
	}
}

func TestRepositoryClaimPending(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)

	queries := &stubQuerier{}
	queries.claimPendingFunc = func(_ context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
		require.Equal(t, int32(2), arg.LimitCount)
		require.Equal(t, "worker-1", arg.LockedBy.String)
		require.Equal(t, now, arg.Before)
		require.True(t, arg.ClaimedAt.Valid)

		headers, err := json.Marshal(map[string]string{"eventName": "catalog.product.created"})
		require.NoError(t, err)

		return []sqlc.OutboxRecord{{
			ID:            uuid.New(),
			EventID:       uuid.New(),
			EventName:     "catalog.product.created",
			AggregateType: "product",
			AggregateID:   "product-1",
			Topic:         "catalog.events",
			Payload:       []byte("payload"),
			Headers:       headers,
			Attempt:       1,
			Status:        commonoutbox.StatusInProgress,
			LockedAt:      sql.NullTime{Time: arg.ClaimedAt.Time, Valid: true},
			LockedBy:      sql.NullString{String: arg.LockedBy.String, Valid: true},
			NextAttemptAt: now,
			MaxAttempts:   20,
			CreatedAt:     now,
			UpdatedAt:     now,
		}}, nil
	}

	repo := NewRepositoryFromQuerier(queries)
	records, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{Limit: 2, Before: now, LockedBy: "worker-1"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "worker-1", records[0].LockedBy)
	require.Equal(t, "catalog.product.created", records[0].Headers["eventName"])
}

func TestRepositoryClaimStaleInProgress(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)

	queries := &stubQuerier{}
	queries.claimStaleInProgressFunc = func(_ context.Context, arg sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
		require.Equal(t, int32(1), arg.LimitCount)
		require.Equal(t, "worker-2", arg.LockedBy.String)
		require.True(t, arg.StaleBefore.Valid)

		return []sqlc.OutboxRecord{{
			ID:            uuid.New(),
			EventID:       uuid.New(),
			EventName:     "catalog.product.created",
			AggregateType: "product",
			AggregateID:   "product-2",
			Topic:         "catalog.events",
			Payload:       []byte("payload"),
			Headers:       []byte(`{"eventName":"catalog.product.created"}`),
			Attempt:       2,
			Status:        commonoutbox.StatusInProgress,
			LockedAt:      sql.NullTime{Time: now, Valid: true},
			LockedBy:      sql.NullString{String: "worker-2", Valid: true},
			NextAttemptAt: now,
			MaxAttempts:   20,
			CreatedAt:     now,
			UpdatedAt:     now,
		}}, nil
	}

	repo := NewRepositoryFromQuerier(queries)
	records, err := repo.ClaimStaleInProgress(context.Background(), commonoutbox.ClaimStaleInProgressParams{Limit: 1, StaleBefore: now, LockedBy: "worker-2"})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "worker-2", records[0].LockedBy)
}

func TestRepositoryMarkOperationsUseOwnershipGuard(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.New()
	claimToken := now.Add(-time.Second)

	queries := &stubQuerier{}

	queries.markPublishedFunc = func(_ context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
		require.Equal(t, id, arg.ID)
		require.Equal(t, "worker-1", arg.LockedBy.String)
		require.Equal(t, claimToken, arg.ClaimToken.Time)
		require.Equal(t, now, arg.PublishedAt.Time)
		return 1, nil
	}

	queries.markRetryableFailureFunc = func(_ context.Context, arg sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error) {
		require.Equal(t, id, arg.ID)
		require.Equal(t, "worker-1", arg.LockedBy.String)
		require.Equal(t, claimToken, arg.ClaimToken.Time)
		require.Equal(t, int32(3), arg.Attempt)
		require.Equal(t, now.Add(time.Second), arg.NextAttemptAt)
		require.Equal(t, "broker down", arg.LastError.String)
		return 1, nil
	}

	queries.markDeadFunc = func(_ context.Context, arg sqlc.MarkOutboxRecordDeadParams) (int64, error) {
		require.Equal(t, id, arg.ID)
		require.Equal(t, "worker-1", arg.LockedBy.String)
		require.Equal(t, claimToken, arg.ClaimToken.Time)
		require.Equal(t, int32(20), arg.Attempt)
		require.Equal(t, "fatal", arg.LastError.String)
		return 1, nil
	}

	repo := NewRepositoryFromQuerier(queries)

	err := repo.MarkPublished(context.Background(), commonoutbox.MarkPublishedParams{
		ID:          id,
		ClaimToken:  claimToken,
		LockedBy:    "worker-1",
		PublishedAt: now,
	})
	require.NoError(t, err)

	err = repo.MarkRetryableFailure(context.Background(), commonoutbox.MarkRetryableFailureParams{
		ID:            id,
		ClaimToken:    claimToken,
		LockedBy:      "worker-1",
		Attempt:       3,
		NextAttemptAt: now.Add(time.Second),
		LastError:     "broker down",
	})
	require.NoError(t, err)

	err = repo.MarkDead(context.Background(), commonoutbox.MarkDeadParams{
		ID:         id,
		ClaimToken: claimToken,
		LockedBy:   "worker-1",
		Attempt:    20,
		LastError:  "fatal",
	})
	require.NoError(t, err)
}

func TestRepositoryMarkOperationsConflict(t *testing.T) {
	queries := &stubQuerier{
		markPublishedFunc: func(_ context.Context, _ sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
			return 0, nil
		},
		markRetryableFailureFunc: func(_ context.Context, _ sqlc.MarkOutboxRecordRetryableFailureParams) (int64, error) {
			return 0, nil
		},
		markDeadFunc: func(_ context.Context, _ sqlc.MarkOutboxRecordDeadParams) (int64, error) {
			return 0, nil
		},
	}

	repo := NewRepositoryFromQuerier(queries)
	now := time.Now().UTC()
	id := uuid.New()

	err := repo.MarkPublished(context.Background(), commonoutbox.MarkPublishedParams{ID: id, ClaimToken: now, LockedBy: "worker-1", PublishedAt: now})
	require.ErrorIs(t, err, commonoutbox.ErrPublishConflict)

	err = repo.MarkRetryableFailure(context.Background(), commonoutbox.MarkRetryableFailureParams{ID: id, ClaimToken: now, LockedBy: "worker-1", Attempt: 1, NextAttemptAt: now.Add(time.Second), LastError: "err"})
	require.ErrorIs(t, err, commonoutbox.ErrPublishConflict)

	err = repo.MarkDead(context.Background(), commonoutbox.MarkDeadParams{ID: id, ClaimToken: now, LockedBy: "worker-1", Attempt: 1, LastError: "err"})
	require.ErrorIs(t, err, commonoutbox.ErrPublishConflict)
}

func TestRepositoryWrapsErrors(t *testing.T) {
	queries := &stubQuerier{
		claimPendingFunc: func(_ context.Context, _ sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
			return nil, errors.New("db down")
		},
		claimStaleInProgressFunc: func(_ context.Context, _ sqlc.ClaimStaleInProgressOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
			return nil, errors.New("db stale down")
		},
	}

	repo := NewRepositoryFromQuerier(queries)
	_, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{Limit: 1, Before: time.Now().UTC(), LockedBy: "worker-1"})
	require.Error(t, err)
	require.ErrorContains(t, err, "claim pending outbox records")

	_, err = repo.ClaimStaleInProgress(context.Background(), commonoutbox.ClaimStaleInProgressParams{Limit: 1, StaleBefore: time.Now().UTC(), LockedBy: "worker-1"})
	require.Error(t, err)
	require.ErrorContains(t, err, "claim stale outbox records")
}
