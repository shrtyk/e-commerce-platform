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
	appendFunc        func(ctx context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error)
	claimPendingFunc  func(ctx context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error)
	markPublishedFunc func(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error)
	markFailedFunc    func(ctx context.Context, arg sqlc.MarkOutboxRecordFailedParams) (int64, error)
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

func (s stubQuerier) MarkOutboxRecordPublished(ctx context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
	if s.markPublishedFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordPublished call")
	}

	return s.markPublishedFunc(ctx, arg)
}

func (s stubQuerier) MarkOutboxRecordFailed(ctx context.Context, arg sqlc.MarkOutboxRecordFailedParams) (int64, error) {
	if s.markFailedFunc == nil {
		return 0, fmt.Errorf("unexpected MarkOutboxRecordFailed call")
	}

	return s.markFailedFunc(ctx, arg)
}

func TestRepositoryAppend(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name        string
		record      commonoutbox.Record
		setup       func(*stubQuerier)
		errIs       error
		errContains string
	}{
		{
			name: "success",
			record: commonoutbox.Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.product.events",
				Key:           []byte("product-1"),
				Payload:       []byte("payload"),
				Headers:       map[string]string{"event_name": "catalog.product.created"},
				Status:        commonoutbox.StatusPending,
			},
			setup: func(q *stubQuerier) {
				q.appendFunc = func(_ context.Context, arg sqlc.AppendOutboxRecordParams) (sqlc.OutboxRecord, error) {
					require.Equal(t, "evt-1", arg.EventID)
					require.Equal(t, string(commonoutbox.StatusPending), arg.Status)

					var headers map[string]string
					err := json.Unmarshal(arg.Headers, &headers)
					require.NoError(t, err)
					require.Equal(t, "catalog.product.created", headers["event_name"])

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
						Attempt:       0,
						Status:        string(commonoutbox.StatusPending),
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
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.product.events",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := &stubQuerier{}
			if tt.setup != nil {
				tt.setup(queries)
			}

			repo := NewRepositoryFromQuerier(queries)
			created, err := repo.Append(context.Background(), tt.record)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				require.Zero(t, created)
				return
			}

			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, created.ID)
			require.Equal(t, commonoutbox.StatusPending, created.Status)
		})
	}
}

func TestRepositoryClaimPending(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	before := now.Add(2 * time.Second)
	claimedAtStart := time.Now().UTC()

	queries := &stubQuerier{
		claimPendingFunc: func(_ context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
			require.Equal(t, int32(2), arg.LimitCount)
			require.True(t, arg.Before.Valid)
			require.Equal(t, before, arg.Before.Time)
			require.True(t, arg.ClaimedAt.Valid)
			require.True(t, arg.StaleBefore.Valid)
			require.WithinDuration(t, arg.ClaimedAt.Time.Add(-claimLockTTL), arg.StaleBefore.Time, 2*time.Millisecond)
			require.False(t, arg.ClaimedAt.Time.Before(claimedAtStart))

			headers, err := json.Marshal(map[string]string{"event_name": "catalog.product.created"})
			require.NoError(t, err)

			return []sqlc.OutboxRecord{{
				ID:            uuid.New(),
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.product.events",
				Payload:       []byte("payload"),
				Headers:       headers,
				Attempt:       1,
				Status:        string(commonoutbox.StatusInProgress),
				LockedAt:      sql.NullTime{Time: arg.ClaimedAt.Time, Valid: true},
				CreatedAt:     now,
				UpdatedAt:     now,
			}}, nil
		},
	}

	repo := NewRepositoryFromQuerier(queries)
	records, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{Limit: 2, Before: before})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, commonoutbox.StatusInProgress, records[0].Status)
	require.Equal(t, "catalog.product.created", records[0].Headers["event_name"])
}

func TestRepositoryClaimPendingDoesNotReclaimFreshInProgress(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	before := now.Add(2 * time.Second)

	staleCandidateID := uuid.New()
	freshInProgressID := uuid.New()

	queries := &stubQuerier{
		claimPendingFunc: func(_ context.Context, arg sqlc.ClaimPendingOutboxRecordsParams) ([]sqlc.OutboxRecord, error) {
			require.True(t, arg.ClaimedAt.Valid)
			require.True(t, arg.StaleBefore.Valid)

			reclaimedLockedAt := arg.StaleBefore.Time.Add(-time.Millisecond)
			freshLockedAt := arg.StaleBefore.Time.Add(time.Millisecond)

			require.True(t, reclaimedLockedAt.Before(arg.StaleBefore.Time))
			require.True(t, freshLockedAt.After(arg.StaleBefore.Time))

			headers, err := json.Marshal(map[string]string{"event_name": "catalog.product.created"})
			require.NoError(t, err)

			// Emulate DB stale branch behavior: return only reclaimed record.
			return []sqlc.OutboxRecord{{
				ID:            staleCandidateID,
				EventID:       "evt-stale",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-stale",
				Topic:         "catalog.product.events",
				Payload:       []byte("payload"),
				Headers:       headers,
				Attempt:       2,
				Status:        string(commonoutbox.StatusInProgress),
				LockedAt:      sql.NullTime{Time: reclaimedLockedAt, Valid: true},
				CreatedAt:     now,
				UpdatedAt:     now,
			}}, nil
		},
	}

	repo := NewRepositoryFromQuerier(queries)
	records, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{Limit: 2, Before: before})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, staleCandidateID, records[0].ID)
	require.NotEqual(t, freshInProgressID, records[0].ID)
}

func TestRepositoryClaimPendingInvalidParams(t *testing.T) {
	repo := NewRepositoryFromQuerier(&stubQuerier{})
	_, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{})
	require.Error(t, err)
	require.ErrorIs(t, err, commonoutbox.ErrInvalidClaimParams)
}

func TestRepositoryMarkPublished(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	tests := []struct {
		name        string
		setup       func(*stubQuerier)
		params      commonoutbox.MarkPublishedParams
		errIs       error
		errContains string
	}{
		{
			name: "success",
			params: commonoutbox.MarkPublishedParams{
				ID:          id,
				ClaimToken:  now.Add(-time.Second),
				PublishedAt: now,
			},
			setup: func(q *stubQuerier) {
				q.markPublishedFunc = func(_ context.Context, arg sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
					require.Equal(t, id, arg.ID)
					require.True(t, arg.ClaimToken.Valid)
					require.Equal(t, now.Add(-time.Second), arg.ClaimToken.Time)
					require.True(t, arg.PublishedAt.Valid)
					return 1, nil
				}
			},
		},
		{
			name:   "invalid params",
			params: commonoutbox.MarkPublishedParams{},
			errIs:  commonoutbox.ErrInvalidMarkPublishedParams,
		},
		{
			name: "conflict",
			params: commonoutbox.MarkPublishedParams{
				ID:          id,
				ClaimToken:  now.Add(-time.Second),
				PublishedAt: now,
			},
			setup: func(q *stubQuerier) {
				q.markPublishedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
					return 0, nil
				}
			},
			errIs: commonoutbox.ErrPublishConflict,
		},
		{
			name: "stale token rejected after reclaim",
			params: commonoutbox.MarkPublishedParams{
				ID:          id,
				ClaimToken:  now.Add(-2 * time.Second),
				PublishedAt: now,
			},
			setup: func(q *stubQuerier) {
				q.markPublishedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
					return 0, nil
				}
			},
			errIs: commonoutbox.ErrPublishConflict,
		},
		{
			name: "current token can finalize",
			params: commonoutbox.MarkPublishedParams{
				ID:          id,
				ClaimToken:  now.Add(-500 * time.Millisecond),
				PublishedAt: now,
			},
			setup: func(q *stubQuerier) {
				q.markPublishedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordPublishedParams) (int64, error) {
					return 1, nil
				}
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
			err := repo.MarkPublished(context.Background(), tt.params)
			if tt.errIs != nil || tt.errContains != "" {
				require.Error(t, err)
				if tt.errIs != nil {
					require.ErrorIs(t, err, tt.errIs)
				}
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRepositoryMarkFailed(t *testing.T) {
	id := uuid.New()
	next := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)

	tests := []struct {
		name   string
		setup  func(*stubQuerier)
		params commonoutbox.MarkFailedParams
		errIs  error
	}{
		{
			name: "success",
			params: commonoutbox.MarkFailedParams{
				ID:            id,
				ClaimToken:    next.Add(-time.Minute),
				Attempt:       2,
				NextAttemptAt: next,
				LastError:     "broker unavailable",
			},
			setup: func(q *stubQuerier) {
				q.markFailedFunc = func(_ context.Context, arg sqlc.MarkOutboxRecordFailedParams) (int64, error) {
					now := time.Now().UTC()
					require.True(t, arg.ClaimToken.Valid)
					require.Equal(t, next.Add(-time.Minute), arg.ClaimToken.Time)
					require.Equal(t, int32(2), arg.Attempt)
					require.Equal(t, "broker unavailable", arg.LastError)
					require.True(t, arg.NextAttemptAt.Valid)
					require.Equal(t, next, arg.NextAttemptAt.Time)
					require.NotEqual(t, next, arg.UpdatedAt)
					require.True(t, arg.UpdatedAt.Before(now) || arg.UpdatedAt.Equal(now))
					return 1, nil
				}
			},
		},
		{
			name:   "invalid params",
			params: commonoutbox.MarkFailedParams{},
			errIs:  commonoutbox.ErrInvalidMarkFailedParams,
		},
		{
			name: "conflict",
			params: commonoutbox.MarkFailedParams{
				ID:            id,
				ClaimToken:    next.Add(-time.Minute),
				Attempt:       1,
				NextAttemptAt: next,
				LastError:     "broker unavailable",
			},
			setup: func(q *stubQuerier) {
				q.markFailedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordFailedParams) (int64, error) {
					return 0, nil
				}
			},
			errIs: commonoutbox.ErrPublishConflict,
		},
		{
			name: "stale token rejected after reclaim",
			params: commonoutbox.MarkFailedParams{
				ID:            id,
				ClaimToken:    next.Add(-2 * time.Minute),
				Attempt:       1,
				NextAttemptAt: next,
				LastError:     "broker unavailable",
			},
			setup: func(q *stubQuerier) {
				q.markFailedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordFailedParams) (int64, error) {
					return 0, nil
				}
			},
			errIs: commonoutbox.ErrPublishConflict,
		},
		{
			name: "current token can finalize",
			params: commonoutbox.MarkFailedParams{
				ID:            id,
				ClaimToken:    next.Add(-time.Minute),
				Attempt:       1,
				NextAttemptAt: next,
				LastError:     "broker unavailable",
			},
			setup: func(q *stubQuerier) {
				q.markFailedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordFailedParams) (int64, error) {
					return 1, nil
				}
			},
		},
		{
			name: "query error wrapped",
			params: commonoutbox.MarkFailedParams{
				ID:            id,
				ClaimToken:    next.Add(-time.Minute),
				Attempt:       1,
				NextAttemptAt: next,
				LastError:     "broker unavailable",
			},
			setup: func(q *stubQuerier) {
				q.markFailedFunc = func(_ context.Context, _ sqlc.MarkOutboxRecordFailedParams) (int64, error) {
					return 0, errors.New("db down")
				}
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
			err := repo.MarkFailed(context.Background(), tt.params)
			if tt.errIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errIs)
				return
			}

			if tt.name == "query error wrapped" {
				require.Error(t, err)
				require.ErrorContains(t, err, "mark outbox record failed")
				return
			}

			require.NoError(t, err)
		})
	}
}
