package kafka

import (
	"context"
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

func TestNewRelayWorkerRejectsNilPublisher(t *testing.T) {
	_, err := NewRelayWorker(&repositoryStub{}, nil, RelayConfig{
		BatchSize:        50,
		Interval:         time.Second,
		RetryBaseBackoff: time.Second,
		RetryMaxBackoff:  10 * time.Second,
		WorkerID:         "worker-1",
		StaleLockTTL:     30 * time.Second,
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "outbox publisher is nil")
}

func TestRelayWorkerTickDisabledPublisherNeverMarksPublished(t *testing.T) {
	now := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	lockedAt := now.Add(-time.Second)
	recordID := uuid.New()

	markPublishedCalled := false
	markRetryCalled := false

	repo := &repositoryStub{
		claimPendingFunc: func(_ context.Context, _ commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error) {
			return []commonoutbox.Record{{ID: recordID, Attempt: 0, MaxAttempts: 5, LockedAt: lockedAt}}, nil
		},
		claimStaleInProgressFunc: func(_ context.Context, _ commonoutbox.ClaimStaleInProgressParams) ([]commonoutbox.Record, error) {
			return nil, nil
		},
		markPublishedFunc: func(_ context.Context, _ commonoutbox.MarkPublishedParams) error {
			markPublishedCalled = true
			return nil
		},
		markRetryableFailureFunc: func(_ context.Context, _ commonoutbox.MarkRetryableFailureParams) error {
			markRetryCalled = true
			return nil
		},
		markDeadFunc: func(_ context.Context, _ commonoutbox.MarkDeadParams) error {
			return nil
		},
	}

	worker, err := NewRelayWorker(repo, NewDisabledPublisher(), RelayConfig{
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
	require.NoError(t, err)
	require.False(t, markPublishedCalled)
	require.True(t, markRetryCalled)
}
