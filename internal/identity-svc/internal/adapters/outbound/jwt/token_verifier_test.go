package jwt

import (
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

func TestTokenVerifierVerify(t *testing.T) {
	issuer := NewTokenIssuer("identity-svc", "secret-key", 15*time.Minute)
	user := domain.User{
		ID:     uuid.New(),
		Role:   domain.UserRoleAdmin,
		Status: domain.UserStatusActive,
	}

	tokenString, err := issuer.IssueToken(user)
	require.NoError(t, err)

	verifier := NewTokenVerifier("secret-key", "identity-svc")
	claims, err := verifier.Verify(tokenString)

	require.NoError(t, err)
	require.Equal(t, httpcommon.Claims{
		UserID: user.ID,
		Role:   string(user.Role),
		Status: string(user.Status),
	}, claims)
}

func TestTokenVerifierVerifyErrors(t *testing.T) {
	tests := []struct {
		name  string
		token string
		setup func() string
	}{
		{
			name: "invalid signature",
			setup: func() string {
				issuer := NewTokenIssuer("identity-svc", "wrong-key", 15*time.Minute)
				token, err := issuer.IssueToken(domain.User{ID: uuid.New(), Role: domain.UserRoleUser, Status: domain.UserStatusActive})
				require.NoError(t, err)
				return token
			},
		},
		{
			name: "malformed token",
			setup: func() string {
				return "not-a-jwt"
			},
		},
		{
			name: "subject is not uuid",
			setup: func() string {
				token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, accessTokenClaims{
					Role:   domain.UserRoleUser,
					Status: domain.UserStatusActive,
					RegisteredClaims: jwtv5.RegisteredClaims{
						Subject:   "bad-uuid",
						ExpiresAt: jwtv5.NewNumericDate(time.Now().Add(15 * time.Minute)),
					},
				})

				tokenString, err := token.SignedString([]byte("secret-key"))
				require.NoError(t, err)
				return tokenString
			},
		},
		{
			name: "issuer mismatch",
			setup: func() string {
				issuer := NewTokenIssuer("other-service", "secret-key", 15*time.Minute)
				token, err := issuer.IssueToken(domain.User{ID: uuid.New(), Role: domain.UserRoleUser, Status: domain.UserStatusActive})
				require.NoError(t, err)
				return token
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := NewTokenVerifier("secret-key", "identity-svc")
			_, err := verifier.Verify(tt.setup())
			require.Error(t, err)
		})
	}
}
