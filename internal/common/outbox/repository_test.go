package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestClaimPendingParamsValidate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		params ClaimPendingParams
		want   error
	}{
		{
			name: "valid",
			params: ClaimPendingParams{
				Limit:    10,
				Before:   now,
				LockedBy: "worker-1",
			},
		},
		{
			name: "zero limit",
			params: ClaimPendingParams{
				Limit:    0,
				Before:   now,
				LockedBy: "worker-1",
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "missing before timestamp",
			params: ClaimPendingParams{
				Limit:    5,
				LockedBy: "worker-1",
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "missing locked by",
			params: ClaimPendingParams{
				Limit:  5,
				Before: now,
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "blank locked by",
			params: ClaimPendingParams{
				Limit:    5,
				Before:   now,
				LockedBy: "   ",
			},
			want: ErrInvalidClaimParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}

func TestClaimStaleInProgressParamsValidate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		params ClaimStaleInProgressParams
		want   error
	}{
		{
			name: "valid",
			params: ClaimStaleInProgressParams{
				Limit:       10,
				StaleBefore: now,
				LockedBy:    "worker-1",
			},
		},
		{
			name: "zero limit",
			params: ClaimStaleInProgressParams{
				Limit:       0,
				StaleBefore: now,
				LockedBy:    "worker-1",
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "missing stale before",
			params: ClaimStaleInProgressParams{
				Limit:    1,
				LockedBy: "worker-1",
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "missing locked by",
			params: ClaimStaleInProgressParams{
				Limit:       1,
				StaleBefore: now,
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "blank locked by",
			params: ClaimStaleInProgressParams{
				Limit:       1,
				StaleBefore: now,
				LockedBy:    "\t",
			},
			want: ErrInvalidClaimParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}

func TestMarkRetryableFailureParamsValidate(t *testing.T) {
	nextAttemptAt := time.Now().UTC().Add(time.Second)

	tests := []struct {
		name   string
		params MarkRetryableFailureParams
		want   error
	}{
		{
			name: "valid",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC().Add(-time.Second),
				LockedBy:      "worker-1",
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
		},
		{
			name: "missing id",
			params: MarkRetryableFailureParams{
				ClaimToken:    time.Now().UTC(),
				LockedBy:      "worker-1",
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
		{
			name: "missing claim token",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				LockedBy:      "worker-1",
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
		{
			name: "non-positive attempt",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				LockedBy:      "worker-1",
				Attempt:       0,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
		{
			name: "empty error message",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				LockedBy:      "worker-1",
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
		{
			name: "missing locked by",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
		{
			name: "blank locked by",
			params: MarkRetryableFailureParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				LockedBy:      "  ",
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkRetryableFailureParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}

func TestMarkPublishedParamsValidate(t *testing.T) {
	publishedAt := time.Now().UTC()

	tests := []struct {
		name   string
		params MarkPublishedParams
		want   error
	}{
		{
			name: "valid",
			params: MarkPublishedParams{
				ID:          uuid.New(),
				ClaimToken:  time.Now().UTC().Add(-time.Second),
				LockedBy:    "worker-1",
				PublishedAt: publishedAt,
			},
		},
		{
			name: "missing id",
			params: MarkPublishedParams{
				ClaimToken:  time.Now().UTC(),
				LockedBy:    "worker-1",
				PublishedAt: publishedAt,
			},
			want: ErrInvalidMarkPublishedParams,
		},
		{
			name: "missing claim token",
			params: MarkPublishedParams{
				ID:          uuid.New(),
				LockedBy:    "worker-1",
				PublishedAt: publishedAt,
			},
			want: ErrInvalidMarkPublishedParams,
		},
		{
			name: "missing published timestamp",
			params: MarkPublishedParams{
				ID:         uuid.New(),
				ClaimToken: time.Now().UTC(),
			},
			want: ErrInvalidMarkPublishedParams,
		},
		{
			name: "missing locked by",
			params: MarkPublishedParams{
				ID:          uuid.New(),
				ClaimToken:  time.Now().UTC(),
				PublishedAt: publishedAt,
			},
			want: ErrInvalidMarkPublishedParams,
		},
		{
			name: "blank locked by",
			params: MarkPublishedParams{
				ID:          uuid.New(),
				ClaimToken:  time.Now().UTC(),
				LockedBy:    "\n",
				PublishedAt: publishedAt,
			},
			want: ErrInvalidMarkPublishedParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}

func TestMarkDeadParamsValidate(t *testing.T) {
	tests := []struct {
		name   string
		params MarkDeadParams
		want   error
	}{
		{
			name: "valid",
			params: MarkDeadParams{
				ID:         uuid.New(),
				ClaimToken: time.Now().UTC(),
				LockedBy:   "worker-1",
				Attempt:    1,
				LastError:  "broker unavailable",
			},
		},
		{
			name: "missing claim token",
			params: MarkDeadParams{
				ID:        uuid.New(),
				LockedBy:  "worker-1",
				Attempt:   1,
				LastError: "broker unavailable",
			},
			want: ErrInvalidMarkDeadParams,
		},
		{
			name: "missing locked by",
			params: MarkDeadParams{
				ID:         uuid.New(),
				ClaimToken: time.Now().UTC(),
				Attempt:    1,
				LastError:  "broker unavailable",
			},
			want: ErrInvalidMarkDeadParams,
		},
		{
			name: "blank locked by",
			params: MarkDeadParams{
				ID:         uuid.New(),
				ClaimToken: time.Now().UTC(),
				LockedBy:   " ",
				Attempt:    1,
				LastError:  "broker unavailable",
			},
			want: ErrInvalidMarkDeadParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}
