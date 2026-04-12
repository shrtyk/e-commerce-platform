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
	claimPendingFunc  func(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error)
	markPublishedFunc func(ctx context.Context, params commonoutbox.MarkPublishedParams) error
	markFailedFunc    func(ctx context.Context, params commonoutbox.MarkFailedParams) error
}

func (s *repositoryStub) ClaimPending(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
	if s.claimPendingFunc == nil {
		return nil, fmt.Errorf("unexpected ClaimPending call")
	}

	return s.claimPendingFunc(ctx, params)
}

func (s *repositoryStub) MarkPublished(ctx context.Context, params commonoutbox.MarkPublishedParams) error {
	if s.markPublishedFunc == nil {
		return fmt.Errorf("unexpected MarkPublished call")
	}

	return s.markPublishedFunc(ctx, params)
}

func (s *repositoryStub) MarkFailed(ctx context.Context, params commonoutbox.MarkFailedParams) error {
	if s.markFailedFunc == nil {
		return fmt.Errorf("unexpected MarkFailed call")
	}

	return s.markFailedFunc(ctx, params)
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

func TestRelayConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		cfg    RelayConfig
		wantOK bool
	}{
		{
			name: "valid",
			cfg: RelayConfig{
				BatchSize:        100,
				Interval:         500 * time.Millisecond,
				RetryBaseBackoff: time.Second,
				RetryMaxBackoff:  30 * time.Second,
			},
			wantOK: true,
		},
		{
			name: "invalid batch size",
			cfg: RelayConfig{
				BatchSize:        0,
				Interval:         500 * time.Millisecond,
				RetryBaseBackoff: time.Second,
				RetryMaxBackoff:  30 * time.Second,
			},
		},
		{
			name: "invalid interval",
			cfg: RelayConfig{
				BatchSize:        10,
				Interval:         0,
				RetryBaseBackoff: time.Second,
				RetryMaxBackoff:  30 * time.Second,
			},
		},
		{
			name: "invalid retry base backoff",
			cfg: RelayConfig{
				BatchSize:        10,
				Interval:         time.Second,
				RetryBaseBackoff: 0,
				RetryMaxBackoff:  30 * time.Second,
			},
		},
		{
			name: "invalid retry max backoff",
			cfg: RelayConfig{
				BatchSize:        10,
				Interval:         time.Second,
				RetryBaseBackoff: time.Second,
				RetryMaxBackoff:  0,
			},
		},
		{
			name: "invalid retry window",
			cfg: RelayConfig{
				BatchSize:        10,
				Interval:         time.Second,
				RetryBaseBackoff: 2 * time.Second,
				RetryMaxBackoff:  time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantOK {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
		})
	}
}

func TestRelayWorkerTick(t *testing.T) {
	now := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		record           commonoutbox.Record
		claimPendingErr  error
		publishErr       error
		markPublishedErr error
		markFailedErr    error
		errContains      string
		assertFn         func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams)
	}{
		{
			name: "mark published when publish success",
			record: commonoutbox.Record{
				ID:       uuid.New(),
				Attempt:  0,
				LockedAt: now,
			},
			assertFn: func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams) {
				require.Len(t, markPublished, 1)
				require.Len(t, markFailed, 0)
				require.Equal(t, now, markPublished[0].ClaimToken)
				require.Equal(t, now, markPublished[0].PublishedAt)
			},
		},
		{
			name: "return error when claim pending fails",
			record: commonoutbox.Record{
				ID:       uuid.New(),
				Attempt:  0,
				LockedAt: now,
			},
			claimPendingErr: errors.New("db unavailable"),
			errContains:     "claim pending outbox records",
			assertFn: func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams) {
				require.Len(t, markPublished, 0)
				require.Len(t, markFailed, 0)
			},
		},
		{
			name: "mark failed with next attempt when publish fails",
			record: commonoutbox.Record{
				ID:       uuid.New(),
				Attempt:  2,
				LockedAt: now,
			},
			publishErr: errors.New("broker unavailable"),
			assertFn: func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams) {
				require.Len(t, markPublished, 0)
				require.Len(t, markFailed, 1)
				require.Equal(t, 3, markFailed[0].Attempt)
				require.Equal(t, now.Add(4*time.Second), markFailed[0].NextAttemptAt)
				require.Contains(t, markFailed[0].LastError, "broker unavailable")
			},
		},
		{
			name: "return error when mark published fails",
			record: commonoutbox.Record{
				ID:       uuid.New(),
				Attempt:  1,
				LockedAt: now,
			},
			markPublishedErr: errors.New("mark published failed"),
			errContains:      "mark outbox record published",
			assertFn: func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams) {
				require.Len(t, markPublished, 1)
				require.Len(t, markFailed, 0)
			},
		},
		{
			name: "return error when mark failed fails",
			record: commonoutbox.Record{
				ID:       uuid.New(),
				Attempt:  1,
				LockedAt: now,
			},
			publishErr:    errors.New("broker unavailable"),
			markFailedErr: errors.New("mark failed error"),
			errContains:   "mark outbox record failed",
			assertFn: func(t *testing.T, markPublished []commonoutbox.MarkPublishedParams, markFailed []commonoutbox.MarkFailedParams) {
				require.Len(t, markPublished, 0)
				require.Len(t, markFailed, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var markPublished []commonoutbox.MarkPublishedParams
			var markFailed []commonoutbox.MarkFailedParams

			repo := &repositoryStub{
				claimPendingFunc: func(_ context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
					require.Equal(t, 50, params.Limit)
					require.Equal(t, now, params.Before)
					if tt.claimPendingErr != nil {
						return nil, tt.claimPendingErr
					}

					return []commonoutbox.Record{tt.record}, nil
				},
				markPublishedFunc: func(_ context.Context, params commonoutbox.MarkPublishedParams) error {
					markPublished = append(markPublished, params)
					return tt.markPublishedErr
				},
				markFailedFunc: func(_ context.Context, params commonoutbox.MarkFailedParams) error {
					markFailed = append(markFailed, params)
					return tt.markFailedErr
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

			tt.assertFn(t, markPublished, markFailed)
		})
	}
}

func TestRelayWorkerTickRetryScheduling(t *testing.T) {
	now := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		attempt          int
		wantAttempt      int
		wantNextAttempt  time.Time
		retryBaseBackoff time.Duration
		retryMaxBackoff  time.Duration
	}{
		{
			name:             "zero attempt starts from base backoff",
			attempt:          0,
			wantAttempt:      1,
			wantNextAttempt:  now.Add(1 * time.Second),
			retryBaseBackoff: 1 * time.Second,
			retryMaxBackoff:  10 * time.Second,
		},
		{
			name:             "low negative attempt uses base backoff",
			attempt:          -1,
			wantAttempt:      0,
			wantNextAttempt:  now.Add(1 * time.Second),
			retryBaseBackoff: 1 * time.Second,
			retryMaxBackoff:  10 * time.Second,
		},
		{
			name:             "high attempt is capped by max backoff",
			attempt:          10,
			wantAttempt:      11,
			wantNextAttempt:  now.Add(10 * time.Second),
			retryBaseBackoff: 1 * time.Second,
			retryMaxBackoff:  10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var markFailed []commonoutbox.MarkFailedParams

			repo := &repositoryStub{
				claimPendingFunc: func(_ context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
					require.Equal(t, 50, params.Limit)
					require.Equal(t, now, params.Before)

					return []commonoutbox.Record{{
						ID:       uuid.New(),
						Attempt:  tt.attempt,
						LockedAt: now,
					}}, nil
				},
				markPublishedFunc: func(_ context.Context, _ commonoutbox.MarkPublishedParams) error {
					return nil
				},
				markFailedFunc: func(_ context.Context, params commonoutbox.MarkFailedParams) error {
					markFailed = append(markFailed, params)
					return nil
				},
			}

			publisher := &publisherStub{publishFunc: func(_ context.Context, _ commonoutbox.Record) error {
				return errors.New("broker unavailable")
			}}

			worker, err := NewRelayWorker(repo, publisher, RelayConfig{
				BatchSize:        50,
				Interval:         time.Second,
				RetryBaseBackoff: tt.retryBaseBackoff,
				RetryMaxBackoff:  tt.retryMaxBackoff,
			})
			require.NoError(t, err)
			worker.now = func() time.Time { return now }

			err = worker.Tick(context.Background())
			require.NoError(t, err)
			require.Len(t, markFailed, 1)
			require.Equal(t, tt.wantAttempt, markFailed[0].Attempt)
			require.Equal(t, tt.wantNextAttempt, markFailed[0].NextAttemptAt)
		})
	}
}

func TestRelayWorkerRunStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	repo := &repositoryStub{
		claimPendingFunc: func(_ context.Context, _ commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
			cancel()
			return nil, nil
		},
		markPublishedFunc: func(_ context.Context, _ commonoutbox.MarkPublishedParams) error {
			return nil
		},
		markFailedFunc: func(_ context.Context, _ commonoutbox.MarkFailedParams) error {
			return nil
		},
	}

	worker, err := NewRelayWorker(repo, &publisherStub{}, RelayConfig{
		BatchSize:        10,
		Interval:         5 * time.Millisecond,
		RetryBaseBackoff: time.Second,
		RetryMaxBackoff:  time.Minute,
	})
	require.NoError(t, err)

	err = worker.Run(ctx)
	require.NoError(t, err)
}
