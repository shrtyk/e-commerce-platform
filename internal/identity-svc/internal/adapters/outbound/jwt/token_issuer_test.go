package jwt

import (
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

func TestTokenIssuerIssueToken(t *testing.T) {
	issuer := NewTokenIssuer("identity-svc", "secret-key", 15*time.Minute)
	user := domain.User{
		ID:     "user-123",
		Email:  "user@example.com",
		Role:   domain.UserRoleAdmin,
		Status: domain.UserStatusActive,
	}

	tokenString, err := issuer.IssueToken(user)
	require.NoError(t, err)
	require.NotEmpty(t, tokenString)

	token, err := jwtv5.ParseWithClaims(tokenString, &accessTokenClaims{}, func(token *jwtv5.Token) (any, error) {
		return []byte("secret-key"), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims, ok := token.Claims.(*accessTokenClaims)
	require.True(t, ok)
	require.Equal(t, "identity-svc", claims.Issuer)
	require.Equal(t, user.ID, claims.Subject)
	require.Equal(t, user.Role, claims.Role)
	require.Equal(t, user.Status, claims.Status)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), claims.ExpiresAt.Time, time.Second)
}
