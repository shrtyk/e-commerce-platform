package outbox

import (
	"context"
	"fmt"
	"strings"
)

type IdempotencyStore interface {
	Acquire(ctx context.Context, key string) (bool, error)
	MarkDone(ctx context.Context, key string) error
}

func ValidateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("idempotency key is required: %w", ErrInvalidIdempotencyKey)
	}

	if key != strings.TrimSpace(key) {
		return fmt.Errorf("idempotency key must not have surrounding whitespace: %w", ErrInvalidIdempotencyKey)
	}

	return nil
}
