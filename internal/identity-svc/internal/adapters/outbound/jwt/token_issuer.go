package jwt

import (
	"errors"
	"fmt"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

type TokenIssuer struct {
	issuer string
	key    []byte
	ttl    time.Duration
}

type accessTokenClaims struct {
	Role   domain.UserRole   `json:"role"`
	Status domain.UserStatus `json:"status"`
	jwtv5.RegisteredClaims
}

func NewTokenIssuer(issuer, key string, ttl time.Duration) *TokenIssuer {
	if strings.TrimSpace(issuer) == "" {
		panic(fmt.Errorf("field \"TokenIssuer.Issuer\" must be non-empty"))
	}

	if strings.TrimSpace(key) == "" {
		panic(fmt.Errorf("field \"TokenIssuer.Key\" must be non-empty"))
	}

	return &TokenIssuer{
		issuer: issuer,
		key:    []byte(key),
		ttl:    ttl,
	}
}

func (i *TokenIssuer) IssueToken(user domain.User) (string, error) {
	if user.ID == uuid.Nil {
		return "", errors.New("issue token: user id is nil")
	}

	now := time.Now().UTC()
	claims := accessTokenClaims{
		Role:   user.Role,
		Status: user.Status,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(i.ttl)),
		},
	}

	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	return token.SignedString(i.key)
}
