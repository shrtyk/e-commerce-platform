package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/stretchr/testify/require"
)

type repositoryStub struct {
	claimPendingFunc         func(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error)
	claimStaleInProgressFunc func(ctx context.Context, params commonoutbox.ClaimStaleInProgressParams) ([]commonoutbox.Record, error)
	markPublishedFunc        func(ctx context.Context, params commonoutbox.MarkPublishedParams) error
	markRetryableFailureFunc func(ctx context.Context, params commonoutbox.MarkRetryableFailureParams) error
	markDeadFunc             func(ctx context.Context, params commonoutbox.MarkDeadParams) error
}

func (s *repositoryStub) ClaimPending(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
	if s.claimPendingFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimPending call")
	}

	return s.claimPendingFunc(ctx, params)
}

func (s *repositoryStub) ClaimStaleInProgress(ctx context.Context, params commonoutbox.ClaimStaleInProgressParams) ([]commonoutbox.Record, error) {
	if s.claimStaleInProgressFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimStaleInProgress call")
	}

	return s.claimStaleInProgressFunc(ctx, params)
}

func (s *repositoryStub) MarkPublished(ctx context.Context, params commonoutbox.MarkPublishedParams) error {
	if s.markPublishedFunc == nil {
		return fmt.Errorf("unexpected MarkPublished call")
	}

	return s.markPublishedFunc(ctx, params)
}

func (s *repositoryStub) MarkRetryableFailure(ctx context.Context, params commonoutbox.MarkRetryableFailureParams) error {
	if s.markRetryableFailureFunc == nil {
		return fmt.Errorf("unexpected MarkRetryableFailure call")
	}

	return s.markRetryableFailureFunc(ctx, params)
}

func (s *repositoryStub) MarkDead(ctx context.Context, params commonoutbox.MarkDeadParams) error {
	if s.markDeadFunc == nil {
		return fmt.Errorf("unexpected MarkDead call")
	}

	return s.markDeadFunc(ctx, params)
}

type publisherStub struct {
	publishFunc func(ctx context.Context, record commonoutbox.Record) error
}

func (s *publisherStub) Publish(ctx context.Context, record commonoutbox.Record) error {
	if s.publishFunc == nil {
		return nil
	}

	return s.publishFunc(ctx, record)
}

func TestRelayWorkerTick(t *testing.T) {
	now := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	lockedAt := now.Add(-time.Second)

	tests := []struct {
		name               string
		pending            []commonoutbox.Record
		stale              []commonoutbox.Record
		claimPendingErr    error
		claimStaleErr      error
		publishErr         error
		markPublishedErr   error
		markRetryableErr   error
		markDeadErr        error
		errContains        string
		expectPublished    int
		expectRetryable    int
		expectDead         int
		retryAttempt       int
		retryNextAttemptAt time.Time
	}{
		{
			name: "published when publish success",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     0,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			expectPublished: 1,
		},
		{
			name:            "claim pending error",
			claimPendingErr: errors.New("db unavailable"),
			errContains:     "claim pending outbox records",
		},
		{
			name: "claim stale error",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     0,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			claimStaleErr: errors.New("db stale unavailable"),
			errContains:   "claim stale outbox records",
		},
		{
			name: "retryable failure",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     2,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			publishErr:         errors.New("broker unavailable"),
			expectRetryable:    1,
			retryAttempt:       3,
			retryNextAttemptAt: now.Add(4 * time.Second),
		},
		{
			name: "retryable failure conflict bubbles",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     1,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			publishErr:         errors.New("broker unavailable"),
			markRetryableErr:   commonoutbox.ErrPublishConflict,
			errContains:        "mark outbox record retryable failure",
			expectRetryable:    1,
			retryAttempt:       2,
			retryNextAttemptAt: now.Add(2 * time.Second),
		},
		{
			name: "retryable failure mark error bubbles",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     0,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			publishErr:         errors.New("broker unavailable"),
			markRetryableErr:   errors.New("store error"),
			errContains:        "mark outbox record retryable failure",
			expectRetryable:    1,
			retryAttempt:       1,
			retryNextAttemptAt: now.Add(1 * time.Second),
		},
		{
			name: "dead when attempts exhausted",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     4,
				MaxAttempts: 5,
				LockedAt:    lockedAt,
			}},
			publishErr: errors.New("broker unavailable"),
			expectDead: 1,
		},
		{
			name: "dead mark conflict bubbles",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     2,
				MaxAttempts: 3,
				LockedAt:    lockedAt,
			}},
			publishErr:  errors.New("broker unavailable"),
			markDeadErr: commonoutbox.ErrPublishConflict,
			errContains: "mark outbox record dead",
			expectDead:  1,
		},
		{
			name: "dead mark error bubbles",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     2,
				MaxAttempts: 3,
				LockedAt:    lockedAt,
			}},
			publishErr:  errors.New("broker unavailable"),
			markDeadErr: errors.New("store dead error"),
			errContains: "mark outbox record dead",
			expectDead:  1,
		},
		{
			name: "stale reclaim record published",
			stale: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     0,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			expectPublished: 1,
		},
		{
			name: "mark published ownership conflict bubbles",
			pending: []commonoutbox.Record{{
				ID:          uuid.New(),
				Attempt:     0,
				MaxAttempts: 20,
				LockedAt:    lockedAt,
			}},
			markPublishedErr: commonoutbox.ErrPublishConflict,
			errContains:      "mark outbox record published",
			expectPublished:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var markPublished []commonoutbox.MarkPublishedParams
			var markRetryable []commonoutbox.MarkRetryableFailureParams
			var markDead []commonoutbox.MarkDeadParams

			repo := &repositoryStub{
				claimPendingFunc: func(_ context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
					require.Equal(t, 50, params.Limit)
					require.Equal(t, now, params.Before)
					require.Equal(t, "worker-1", params.LockedBy)
					if tt.claimPendingErr != nil {
						return nil, tt.claimPendingErr
					}

					return tt.pending, nil
				},
				claimStaleInProgressFunc: func(_ context.Context, params commonoutbox.ClaimStaleInProgressParams) ([]commonoutbox.Record, error) {
					require.Equal(t, 50, params.Limit)
					require.Equal(t, now.Add(-30*time.Second), params.StaleBefore)
					require.Equal(t, "worker-1", params.LockedBy)
					if tt.claimStaleErr != nil {
						return nil, tt.claimStaleErr
					}

					return tt.stale, nil
				},
				markPublishedFunc: func(_ context.Context, params commonoutbox.MarkPublishedParams) error {
					markPublished = append(markPublished, params)
					return tt.markPublishedErr
				},
				markRetryableFailureFunc: func(_ context.Context, params commonoutbox.MarkRetryableFailureParams) error {
					markRetryable = append(markRetryable, params)
					return tt.markRetryableErr
				},
				markDeadFunc: func(_ context.Context, params commonoutbox.MarkDeadParams) error {
					markDead = append(markDead, params)
					return tt.markDeadErr
				},
			}

			publisher := &publisherStub{publishFunc: func(_ context.Context, _ commonoutbox.Record) error {
				return tt.publishErr
			}}

			worker, err := NewRelayWorker(repo, publisher, RelayConfig{
				BatchSize:        50,
				Interval:         time.Second,
				RetryBaseBackoff: time.Second,
				RetryMaxBackoff:  10 * time.Second,
				WorkerID:         "worker-1",
				StaleLockTTL:     30 * time.Second,
			})
			require.NoError(t, err)
			worker.now = func() time.Time { return now }

			err = worker.Tick(context.Background())
			if tt.errContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, markPublished, tt.expectPublished)
			require.Len(t, markRetryable, tt.expectRetryable)
			require.Len(t, markDead, tt.expectDead)

			if tt.expectPublished > 0 {
				require.Equal(t, "worker-1", markPublished[0].LockedBy)
				require.Equal(t, lockedAt, markPublished[0].ClaimToken)
				require.Equal(t, now, markPublished[0].PublishedAt)
			}

			if tt.expectRetryable > 0 {
				require.Equal(t, "worker-1", markRetryable[0].LockedBy)
				require.Equal(t, tt.retryAttempt, markRetryable[0].Attempt)
				require.Equal(t, tt.retryNextAttemptAt, markRetryable[0].NextAttemptAt)
			}

			if tt.expectDead > 0 {
				require.Equal(t, "worker-1", markDead[0].LockedBy)
				require.Equal(t, tt.pending[0].Attempt+1, markDead[0].Attempt)
			}
		})
	}
}
