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
	ClaimStaleInProgress(ctx context.Context, params ClaimStaleInProgressParams) ([]Record, error)
	MarkPublished(ctx context.Context, params MarkPublishedParams) error
	MarkRetryableFailure(ctx context.Context, params MarkRetryableFailureParams) error
	MarkDead(ctx context.Context, params MarkDeadParams) error
}

type ClaimPendingParams struct {
	Limit    int
	Before   time.Time
	LockedBy string
}

func (p ClaimPendingParams) Validate() error {
	if p.Limit < 1 {
		return fmt.Errorf("limit must be positive: %w", ErrInvalidClaimParams)
	}

	if p.Before.IsZero() {
		return fmt.Errorf("before timestamp is required: %w", ErrInvalidClaimParams)
	}

	if strings.TrimSpace(p.LockedBy) == "" {
		return fmt.Errorf("locked by is required: %w", ErrInvalidClaimParams)
	}

	return nil
}

type ClaimStaleInProgressParams struct {
	Limit       int
	StaleBefore time.Time
	LockedBy    string
}

func (p ClaimStaleInProgressParams) Validate() error {
	if p.Limit < 1 {
		return fmt.Errorf("limit must be positive: %w", ErrInvalidClaimParams)
	}

	if p.StaleBefore.IsZero() {
		return fmt.Errorf("stale_before timestamp is required: %w", ErrInvalidClaimParams)
	}

	if strings.TrimSpace(p.LockedBy) == "" {
		return fmt.Errorf("locked by is required: %w", ErrInvalidClaimParams)
	}

	return nil
}

type MarkPublishedParams struct {
	ID          uuid.UUID
	ClaimToken  time.Time
	LockedBy    string
	PublishedAt time.Time
}

func (p MarkPublishedParams) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("id is required: %w", ErrInvalidMarkPublishedParams)
	}

	if p.ClaimToken.IsZero() {
		return fmt.Errorf("claim token is required: %w", ErrInvalidMarkPublishedParams)
	}

	if p.PublishedAt.IsZero() {
		return fmt.Errorf("published timestamp is required: %w", ErrInvalidMarkPublishedParams)
	}

	if strings.TrimSpace(p.LockedBy) == "" {
		return fmt.Errorf("locked by is required: %w", ErrInvalidMarkPublishedParams)
	}

	return nil
}

type MarkRetryableFailureParams struct {
	ID            uuid.UUID
	ClaimToken    time.Time
	LockedBy      string
	Attempt       int
	NextAttemptAt time.Time
	LastError     string
}

func (p MarkRetryableFailureParams) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("id is required: %w", ErrInvalidMarkRetryableFailureParams)
	}

	if p.ClaimToken.IsZero() {
		return fmt.Errorf("claim token is required: %w", ErrInvalidMarkRetryableFailureParams)
	}

	if strings.TrimSpace(p.LockedBy) == "" {
		return fmt.Errorf("locked by is required: %w", ErrInvalidMarkRetryableFailureParams)
	}

	if p.Attempt < 1 {
		return fmt.Errorf("attempt must be positive: %w", ErrInvalidMarkRetryableFailureParams)
	}

	if p.NextAttemptAt.IsZero() {
		return fmt.Errorf("next attempt timestamp is required: %w", ErrInvalidMarkRetryableFailureParams)
	}

	if strings.TrimSpace(p.LastError) == "" {
		return fmt.Errorf("last error is required: %w", ErrInvalidMarkRetryableFailureParams)
	}

	return nil
}

type MarkDeadParams struct {
	ID         uuid.UUID
	ClaimToken time.Time
	LockedBy   string
	Attempt    int
	LastError  string
}

func (p MarkDeadParams) Validate() error {
	if p.ID == uuid.Nil {
		return fmt.Errorf("id is required: %w", ErrInvalidMarkDeadParams)
	}

	if p.ClaimToken.IsZero() {
		return fmt.Errorf("claim token is required: %w", ErrInvalidMarkDeadParams)
	}

	if strings.TrimSpace(p.LockedBy) == "" {
		return fmt.Errorf("locked by is required: %w", ErrInvalidMarkDeadParams)
	}

	if p.Attempt < 1 {
		return fmt.Errorf("attempt must be positive: %w", ErrInvalidMarkDeadParams)
	}

	if strings.TrimSpace(p.LastError) == "" {
		return fmt.Errorf("last error is required: %w", ErrInvalidMarkDeadParams)
	}

	return nil
}
