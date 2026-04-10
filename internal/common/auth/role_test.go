package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoleIsValid(t *testing.T) {
	tests := []struct {
		name string
		role Role
		ok   bool
	}{
		{name: "user", role: RoleUser, ok: true},
		{name: "admin", role: RoleAdmin, ok: true},
		{name: "unknown", role: RoleUnknown, ok: false},
		{name: "unsupported", role: Role("guest"), ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.role.IsValid())
		})
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Role
		err   error
	}{
		{name: "user", input: "user", want: RoleUser},
		{name: "admin", input: "admin", want: RoleAdmin},
		{name: "trim spaces", input: "  user  ", want: RoleUser},
		{name: "empty", input: "", err: ErrInvalidRole},
		{name: "unsupported", input: "guest", err: ErrInvalidRole},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, err := ParseRole(tt.input)
			if tt.err == nil {
				require.NoError(t, err)
				require.Equal(t, tt.want, role)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, RoleUnknown, role)
		})
	}
}

func TestHasAnyRole(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		allowed []Role
		ok      bool
	}{
		{name: "allowed role", role: RoleUser, allowed: []Role{RoleAdmin, RoleUser}, ok: true},
		{name: "role is not in allowed list", role: RoleUser, allowed: []Role{RoleAdmin}, ok: false},
		{name: "no required role", role: RoleUser, allowed: nil, ok: true},
		{name: "invalid role", role: RoleUnknown, allowed: []Role{RoleUser}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, HasAnyRole(tt.role, tt.allowed...))
		})
	}
}
