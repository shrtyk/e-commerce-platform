package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusIsValid(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		ok     bool
	}{
		{name: "active", status: StatusActive, ok: true},
		{name: "disabled", status: StatusDisabled, ok: true},
		{name: "unknown", status: StatusUnknown, ok: false},
		{name: "unsupported", status: Status("blocked"), ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.status.IsValid())
		})
	}
}

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Status
		err   error
	}{
		{name: "active", input: "active", want: StatusActive},
		{name: "disabled", input: "disabled", want: StatusDisabled},
		{name: "trim spaces", input: "  active  ", want: StatusActive},
		{name: "empty", input: "", err: ErrInvalidStatus},
		{name: "unsupported", input: "blocked", err: ErrInvalidStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := ParseStatus(tt.input)
			if tt.err == nil {
				require.NoError(t, err)
				require.Equal(t, tt.want, status)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, StatusUnknown, status)
		})
	}
}

func TestStatusCanAuthenticate(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		ok     bool
	}{
		{name: "active", status: StatusActive, ok: true},
		{name: "disabled", status: StatusDisabled, ok: false},
		{name: "unknown", status: StatusUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.status.CanAuthenticate())
		})
	}
}
