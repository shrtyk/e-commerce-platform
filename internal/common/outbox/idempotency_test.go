package outbox

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateIdempotencyKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want error
	}{
		{name: "valid", key: "payment:order-1:evt-1"},
		{name: "empty", key: "", want: ErrInvalidIdempotencyKey},
		{name: "whitespace", key: "   ", want: ErrInvalidIdempotencyKey},
		{name: "leading whitespace", key: " key", want: ErrInvalidIdempotencyKey},
		{name: "trailing whitespace", key: "key ", want: ErrInvalidIdempotencyKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdempotencyKey(tt.key)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want))
		})
	}
}
