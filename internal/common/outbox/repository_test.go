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
				Limit:  10,
				Before: now,
			},
		},
		{
			name: "zero limit",
			params: ClaimPendingParams{
				Limit:  0,
				Before: now,
			},
			want: ErrInvalidClaimParams,
		},
		{
			name: "missing before timestamp",
			params: ClaimPendingParams{
				Limit: 5,
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

func TestMarkFailedParamsValidate(t *testing.T) {
	nextAttemptAt := time.Now().UTC().Add(time.Second)

	tests := []struct {
		name   string
		params MarkFailedParams
		want   error
	}{
		{
			name: "valid",
			params: MarkFailedParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
		},
		{
			name: "missing id",
			params: MarkFailedParams{
				ClaimToken:    time.Now().UTC(),
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkFailedParams,
		},
		{
			name: "missing claim token",
			params: MarkFailedParams{
				ID:            uuid.New(),
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkFailedParams,
		},
		{
			name: "non-positive attempt",
			params: MarkFailedParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				Attempt:       0,
				NextAttemptAt: nextAttemptAt,
				LastError:     "broker unavailable",
			},
			want: ErrInvalidMarkFailedParams,
		},
		{
			name: "empty error message",
			params: MarkFailedParams{
				ID:            uuid.New(),
				ClaimToken:    time.Now().UTC(),
				Attempt:       1,
				NextAttemptAt: nextAttemptAt,
			},
			want: ErrInvalidMarkFailedParams,
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
				ClaimToken:  time.Now().UTC(),
				PublishedAt: publishedAt,
			},
		},
		{
			name: "missing id",
			params: MarkPublishedParams{
				ClaimToken:  time.Now().UTC(),
				PublishedAt: publishedAt,
			},
			want: ErrInvalidMarkPublishedParams,
		},
		{
			name: "missing claim token",
			params: MarkPublishedParams{
				ID:          uuid.New(),
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
