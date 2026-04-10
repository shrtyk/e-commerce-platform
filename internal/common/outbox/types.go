package outbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusPublished  Status = "published"
	StatusFailed     Status = "failed"
)

type Record struct {
	ID uuid.UUID

	EventID       string
	EventName     string
	AggregateType string
	AggregateID   string

	Topic   string
	Key     []byte
	Payload []byte
	Headers map[string]string

	Attempt       int
	Status        Status
	LastError     string
	NextAttemptAt time.Time
	LockedAt      time.Time
	PublishedAt   time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusInProgress, StatusPublished, StatusFailed:
		return true
	default:
		return false
	}
}

func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusPending:
		return next == StatusInProgress
	case StatusInProgress:
		return next == StatusPublished || next == StatusFailed
	case StatusFailed:
		return next == StatusInProgress
	default:
		return false
	}
}

func (r Record) ValidateForAppend() error {
	if r.ID != uuid.Nil {
		return fmt.Errorf("id must be empty on append: %w", ErrInvalidRecord)
	}

	if strings.TrimSpace(r.EventID) == "" {
		return fmt.Errorf("event id is required: %w", ErrInvalidRecord)
	}

	if strings.TrimSpace(r.EventName) == "" {
		return fmt.Errorf("event name is required: %w", ErrInvalidRecord)
	}

	if strings.TrimSpace(r.AggregateType) == "" {
		return fmt.Errorf("aggregate type is required: %w", ErrInvalidRecord)
	}

	if strings.TrimSpace(r.AggregateID) == "" {
		return fmt.Errorf("aggregate id is required: %w", ErrInvalidRecord)
	}

	if strings.TrimSpace(r.Topic) == "" {
		return fmt.Errorf("topic is required: %w", ErrInvalidRecord)
	}

	if len(r.Payload) == 0 {
		return fmt.Errorf("payload is required: %w", ErrInvalidRecord)
	}

	if r.Attempt != 0 {
		return fmt.Errorf("attempt must be zero on append: %w", ErrInvalidRecord)
	}

	if r.Status != StatusPending {
		return fmt.Errorf("status must be pending on append: %w", ErrInvalidRecord)
	}

	if r.LastError != "" || !r.NextAttemptAt.IsZero() || !r.LockedAt.IsZero() || !r.PublishedAt.IsZero() || !r.CreatedAt.IsZero() || !r.UpdatedAt.IsZero() {
		return fmt.Errorf("record contains adapter-managed metadata: %w", ErrInvalidRecord)
	}

	for key := range r.Headers {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("header key is required: %w", ErrInvalidRecord)
		}
	}

	return nil
}
