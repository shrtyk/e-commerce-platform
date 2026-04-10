package outbox

import (
	"context"
	"fmt"
	"time"
)

type Publisher interface {
	Publish(ctx context.Context, record Record) error
}

type Worker interface {
	Run(ctx context.Context) error
}

type RelayCycle interface {
	Tick(ctx context.Context) error
}

type RelayConfig struct {
	BatchSize int
	Interval  time.Duration
}

func (c RelayConfig) Validate() error {
	if c.BatchSize < 1 {
		return fmt.Errorf("batch size must be positive: %w", ErrInvalidRelayConfig)
	}

	if c.Interval <= 0 {
		return fmt.Errorf("interval must be positive: %w", ErrInvalidRelayConfig)
	}

	return nil
}
