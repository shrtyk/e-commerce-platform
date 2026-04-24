package jwt

import (
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

func TestTokenIssuerIssueToken(t *testing.T) {
	issuer := NewTokenIssuer("identity-svc", "secret-key", 15*time.Minute)
	user := domain.User{
		ID:     uuid.New(),
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
	require.Equal(t, user.ID.String(), claims.Subject)
	require.Equal(t, user.Role, claims.Role)
	require.Equal(t, user.Status, claims.Status)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), claims.ExpiresAt.Time, time.Second)
}

func TestTokenIssuerIssueTokenRejectsNilUserID(t *testing.T) {
	issuer := NewTokenIssuer("identity-svc", "secret-key", 15*time.Minute)

	tokenString, err := issuer.IssueToken(domain.User{ID: uuid.Nil})

	require.ErrorContains(t, err, "user id is nil")
	require.Empty(t, tokenString)
}

func TestNewTokenIssuerPanicsWhenSecretOrIssuerInvalid(t *testing.T) {
	tests := []struct {
		name    string
		issuer  string
		key     string
		wantErr string
	}{
		{name: "empty issuer", issuer: "", key: "secret-key", wantErr: "field \"TokenIssuer.Issuer\" must be non-empty"},
		{name: "whitespace issuer", issuer: "   ", key: "secret-key", wantErr: "field \"TokenIssuer.Issuer\" must be non-empty"},
		{name: "empty key", issuer: "identity-svc", key: "", wantErr: "field \"TokenIssuer.Key\" must be non-empty"},
		{name: "whitespace key", issuer: "identity-svc", key: "   ", wantErr: "field \"TokenIssuer.Key\" must be non-empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.PanicsWithError(t, tt.wantErr, func() {
				_ = NewTokenIssuer(tt.issuer, tt.key, 15*time.Minute)
			})
		})
	}
}
