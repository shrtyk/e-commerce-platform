package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRecordValidateForAppend(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   error
	}{
		{
			name: "valid",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Headers: map[string]string{
					"content-type": "application/protobuf",
				},
				Status: StatusPending,
			},
		},
		{
			name: "missing event id",
			record: Record{
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "missing event name",
			record: Record{
				EventID:       "evt-1",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "missing aggregate type",
			record: Record{
				EventID:     "evt-1",
				EventName:   "catalog.product.created",
				AggregateID: "product-1",
				Topic:       "catalog.events.v1",
				Payload:     []byte("payload"),
				Status:      StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "missing aggregate id",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "missing topic",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "missing payload",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects non-zero attempt",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Attempt:       1,
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "headers contain empty key",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Headers: map[string]string{
					"": "value",
				},
				Status: StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects last error",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects next attempt timestamp",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				NextAttemptAt: time.Now().UTC(),
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects locked timestamp",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				LockedAt:      time.Now().UTC(),
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects published timestamp",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				PublishedAt:   time.Now().UTC(),
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects created timestamp",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				CreatedAt:     time.Now().UTC(),
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects updated timestamp",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
				UpdatedAt:     time.Now().UTC(),
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append rejects adapter managed fields",
			record: Record{
				ID:            uuid.New(),
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusPending,
			},
			want: ErrInvalidRecord,
		},
		{
			name: "append requires pending status",
			record: Record{
				EventID:       "evt-1",
				EventName:     "catalog.product.created",
				AggregateType: "product",
				AggregateID:   "product-1",
				Topic:         "catalog.events.v1",
				Payload:       []byte("payload"),
				Status:        StatusFailed,
			},
			want: ErrInvalidRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.record.ValidateForAppend()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}

func TestStatusIsValid(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		ok     bool
	}{
		{name: "pending", status: StatusPending, ok: true},
		{name: "in progress", status: StatusInProgress, ok: true},
		{name: "published", status: StatusPublished, ok: true},
		{name: "failed", status: StatusFailed, ok: true},
		{name: "empty", status: Status(""), ok: false},
		{name: "unknown", status: Status("unknown"), ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.status.IsValid())
		})
	}
}

func TestStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from Status
		to   Status
		ok   bool
	}{
		{name: "pending to in progress", from: StatusPending, to: StatusInProgress, ok: true},
		{name: "in progress to published", from: StatusInProgress, to: StatusPublished, ok: true},
		{name: "in progress to failed", from: StatusInProgress, to: StatusFailed, ok: true},
		{name: "failed to in progress", from: StatusFailed, to: StatusInProgress, ok: true},
		{name: "pending to published", from: StatusPending, to: StatusPublished, ok: false},
		{name: "published to failed", from: StatusPublished, to: StatusFailed, ok: false},
		{name: "unknown source", from: Status("unknown"), to: StatusPending, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.from.CanTransitionTo(tt.to))
		})
	}
}
