package auth_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/common/auth"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

func TestClaimsValidate(t *testing.T) {
	tests := []struct {
		name   string
		claims auth.Claims
		err    error
	}{
		{
			name: "valid",
			claims: auth.Claims{
				UserID: uuid.New(),
				Role:   auth.RoleUser,
				Status: auth.StatusActive,
			},
		},
		{
			name: "empty user id",
			claims: auth.Claims{
				Role:   auth.RoleUser,
				Status: auth.StatusActive,
			},
			err: auth.ErrInvalidClaims,
		},
		{
			name: "invalid role",
			claims: auth.Claims{
				UserID: uuid.New(),
				Role:   auth.RoleUnknown,
				Status: auth.StatusActive,
			},
			err: auth.ErrInvalidClaims,
		},
		{
			name: "invalid status",
			claims: auth.Claims{
				UserID: uuid.New(),
				Role:   auth.RoleUser,
				Status: auth.StatusUnknown,
			},
			err: auth.ErrInvalidClaims,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.claims.Validate()
			if tt.err == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
		})
	}
}

func TestClaimsCanAccess(t *testing.T) {
	validClaims := auth.Claims{UserID: uuid.New(), Role: auth.RoleAdmin, Status: auth.StatusActive}

	tests := []struct {
		name         string
		claims       auth.Claims
		allowedRoles []auth.Role
		ok           bool
	}{
		{name: "allowed role", claims: validClaims, allowedRoles: []auth.Role{auth.RoleAdmin}, ok: true},
		{name: "no required role", claims: validClaims, allowedRoles: nil, ok: true},
		{name: "role denied", claims: validClaims, allowedRoles: []auth.Role{auth.RoleUser}, ok: false},
		{name: "disabled status", claims: auth.Claims{UserID: uuid.New(), Role: auth.RoleAdmin, Status: auth.StatusDisabled}, allowedRoles: []auth.Role{auth.RoleAdmin}, ok: false},
		{name: "invalid claims", claims: auth.Claims{Role: auth.RoleAdmin, Status: auth.StatusActive}, allowedRoles: []auth.Role{auth.RoleAdmin}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, tt.claims.CanAccess(tt.allowedRoles...))
		})
	}
}

func TestToAuthClaims(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name   string
		claims transport.Claims
		err    error
	}{
		{
			name: "valid",
			claims: transport.Claims{
				UserID: userID,
				Role:   "admin",
				Status: "active",
			},
		},
		{
			name: "normalized role and status",
			claims: transport.Claims{
				UserID: userID,
				Role:   " Admin ",
				Status: " ACTIVE ",
			},
		},
		{
			name: "invalid role",
			claims: transport.Claims{
				UserID: userID,
				Role:   "guest",
				Status: "active",
			},
			err: auth.ErrInvalidRole,
		},
		{
			name: "invalid status",
			claims: transport.Claims{
				UserID: userID,
				Role:   "user",
				Status: "blocked",
			},
			err: auth.ErrInvalidStatus,
		},
		{
			name: "missing user id",
			claims: transport.Claims{
				Role:   "user",
				Status: "active",
			},
			err: auth.ErrInvalidClaims,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := transport.ToAuthClaims(tt.claims)
			if tt.err == nil {
				require.NoError(t, err)
				require.Equal(t, tt.claims.UserID, claims.UserID)
				require.True(t, claims.Role.IsValid())
				require.True(t, claims.Status.IsValid())
				return
			}

			require.Error(t, err)
			require.True(t, errors.Is(err, tt.err))
			require.Equal(t, auth.Claims{}, claims)
		})
	}
}

func TestFromAuthClaims(t *testing.T) {
	authClaims := auth.Claims{
		UserID: uuid.New(),
		Role:   auth.RoleUser,
		Status: auth.StatusActive,
	}

	transportClaims, err := transport.FromAuthClaims(authClaims)
	require.NoError(t, err)

	require.Equal(t, authClaims.UserID, transportClaims.UserID)
	require.Equal(t, string(authClaims.Role), transportClaims.Role)
	require.Equal(t, string(authClaims.Status), transportClaims.Status)
}

func TestFromAuthClaimsInvalidClaims(t *testing.T) {
	transportClaims, err := transport.FromAuthClaims(auth.Claims{Role: auth.RoleAdmin, Status: auth.StatusActive})

	require.Error(t, err)
	require.True(t, errors.Is(err, auth.ErrInvalidClaims))
	require.Equal(t, transport.Claims{}, transportClaims)
}
