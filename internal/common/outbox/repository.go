package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Append(ctx context.Context, record Record) (Record, error)
	ClaimPending(ctx context.Context, params ClaimPendingParams) ([]Record, error)
	MarkPublished(ctx context.Context, params MarkPublishedParams) error
	MarkFailed(ctx context.Context, params MarkFailedParams) error
}

type ClaimPendingParams struct {
	Limit  int
	Before time.Time
}

func (p ClaimPendingParams) Validate() error {
	if p.Limit < 1 {
		return fmt.Errorf("limit must be positive: %w", ErrInvalidClaimParams)
	}

	if p.Before.IsZero() {
		return fmt.Errorf("before timestamp is required: %w", ErrInvalidClaimParams)
	}

	return nil
}

type MarkPublishedParams struct {
	ID          uuid.UUID
	PublishedAt time.Time
}

func (p MarkPublishedParams) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("id is required: %w", ErrInvalidMarkPublishedParams)
	}

	if p.PublishedAt.IsZero() {
		return fmt.Errorf("published timestamp is required: %w", ErrInvalidMarkPublishedParams)
	}

	return nil
}

type MarkFailedParams struct {
	ID            uuid.UUID
	Attempt       int
	NextAttemptAt time.Time
	LastError     string
}

func (p MarkFailedParams) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("id is required: %w", ErrInvalidMarkFailedParams)
	}

	if p.Attempt < 1 {
		return fmt.Errorf("attempt must be positive: %w", ErrInvalidMarkFailedParams)
	}

	if p.NextAttemptAt.IsZero() {
		return fmt.Errorf("next attempt timestamp is required: %w", ErrInvalidMarkFailedParams)
	}

	if strings.TrimSpace(p.LastError) == "" {
		return fmt.Errorf("last error is required: %w", ErrInvalidMarkFailedParams)
	}

	return nil
}
