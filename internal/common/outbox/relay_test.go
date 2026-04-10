package outbox

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRelayConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config RelayConfig
		want   error
	}{
		{
			name: "valid",
			config: RelayConfig{
				BatchSize: 10,
				Interval:  100 * time.Millisecond,
			},
		},
		{
			name: "invalid batch size",
			config: RelayConfig{
				BatchSize: 0,
				Interval:  100 * time.Millisecond,
			},
			want: ErrInvalidRelayConfig,
		},
		{
			name: "invalid interval",
			config: RelayConfig{
				BatchSize: 10,
				Interval:  -1 * time.Second,
			},
			want: ErrInvalidRelayConfig,
		},
		{
			name: "zero interval",
			config: RelayConfig{
				BatchSize: 10,
				Interval:  0,
			},
			want: ErrInvalidRelayConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}
