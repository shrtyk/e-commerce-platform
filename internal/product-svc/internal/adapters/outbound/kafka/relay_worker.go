package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
)

type outboxRepository interface {
	ClaimPending(ctx context.Context, params commonoutbox.ClaimPendingParams) ([]commonoutbox.Record, error)
	MarkPublished(ctx context.Context, params commonoutbox.MarkPublishedParams) error
	MarkFailed(ctx context.Context, params commonoutbox.MarkFailedParams) error
}

type outboxPublisher interface {
	Publish(ctx context.Context, record commonoutbox.Record) error
}

type RelayConfig struct {
	BatchSize        int
	Interval         time.Duration
	RetryBaseBackoff time.Duration
	RetryMaxBackoff  time.Duration
}

func (c RelayConfig) Validate() error {
	if c.BatchSize < 1 {
		return fmt.Errorf("batch size must be positive")
	}

	if c.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}

	if c.RetryBaseBackoff <= 0 {
		return fmt.Errorf("retry base backoff must be positive")
	}

	if c.RetryMaxBackoff <= 0 {
		return fmt.Errorf("retry max backoff must be positive")
	}

	if c.RetryBaseBackoff > c.RetryMaxBackoff {
		return fmt.Errorf("retry base backoff must be <= retry max backoff")
	}

	return nil
}

type RelayWorker struct {
	repository outboxRepository
	publisher  outboxPublisher
	config     RelayConfig
	now        func() time.Time
	ticker     func(time.Duration) ticker
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type stdTicker struct {
	inner *time.Ticker
}

func (t stdTicker) C() <-chan time.Time {
	return t.inner.C
}

func (t stdTicker) Stop() {
	t.inner.Stop()
}

func NewRelayWorker(repository outboxRepository, publisher outboxPublisher, cfg RelayConfig) (*RelayWorker, error) {
	if repository == nil {
		return nil, fmt.Errorf("outbox repository is nil")
	}

	if publisher == nil {
		return nil, fmt.Errorf("outbox publisher is nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate relay config: %w", err)
	}

	return &RelayWorker{
		repository: repository,
		publisher:  publisher,
		config:     cfg,
		now:        time.Now,
		ticker: func(d time.Duration) ticker {
			return stdTicker{inner: time.NewTicker(d)}
		},
	}, nil
}

func (w *RelayWorker) Run(ctx context.Context) error {
	if err := w.Tick(ctx); err != nil {
		return err
	}

	t := w.ticker(w.config.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C():
			if err := w.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *RelayWorker) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nil
	}

	now := w.now().UTC()

	claimed, err := w.repository.ClaimPending(ctx, commonoutbox.ClaimPendingParams{
		Limit:  w.config.BatchSize,
		Before: now,
	})
	if err != nil {
		return fmt.Errorf("claim pending outbox records: %w", err)
	}

	for _, record := range claimed {
		if err := ctx.Err(); err != nil {
			return nil
		}

		if err := w.publishOne(ctx, record, now); err != nil {
			return err
		}
	}

	return nil
}

func (w *RelayWorker) publishOne(ctx context.Context, record commonoutbox.Record, now time.Time) error {
	if err := w.publisher.Publish(ctx, record); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return nil
		}

		nextAttemptAt := now.Add(w.backoffForAttempt(record.Attempt + 1))

		if markErr := w.repository.MarkFailed(ctx, commonoutbox.MarkFailedParams{
			ID:            record.ID,
			ClaimToken:    record.LockedAt.UTC(),
			Attempt:       record.Attempt + 1,
			NextAttemptAt: nextAttemptAt,
			LastError:     err.Error(),
		}); markErr != nil {
			if errors.Is(markErr, context.Canceled) || errors.Is(markErr, context.DeadlineExceeded) || ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("mark outbox record failed: %w", markErr)
		}

		return nil
	}

	if err := w.repository.MarkPublished(ctx, commonoutbox.MarkPublishedParams{
		ID:          record.ID,
		ClaimToken:  record.LockedAt.UTC(),
		PublishedAt: now,
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf("mark outbox record published: %w", err)
	}

	return nil
}

func (w *RelayWorker) backoffForAttempt(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	backoff := w.config.RetryBaseBackoff
	for i := 1; i < attempt; i++ {
		if backoff >= w.config.RetryMaxBackoff {
			return w.config.RetryMaxBackoff
		}

		backoff *= 2
	}

	if backoff > w.config.RetryMaxBackoff {
		return w.config.RetryMaxBackoff
	}

	return backoff
}
