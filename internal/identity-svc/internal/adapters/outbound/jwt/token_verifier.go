package jwt

import (
	"fmt"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/http"
)

type TokenVerifier struct {
	key    []byte
	issuer string
}

type tokenVerifierClaims struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	jwtv5.RegisteredClaims
}

func NewTokenVerifier(key, issuer string) *TokenVerifier {
	return &TokenVerifier{key: []byte(key), issuer: issuer}
}

func (v *TokenVerifier) Verify(token string) (httpcommon.Claims, error) {
	parsedToken, err := jwtv5.ParseWithClaims(token, &tokenVerifierClaims{}, func(parsedToken *jwtv5.Token) (any, error) {
		if parsedToken.Method.Alg() != jwtv5.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", parsedToken.Method.Alg())
		}

		return v.key, nil
	}, jwtv5.WithIssuer(v.issuer))
	if err != nil {
		return httpcommon.Claims{}, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := parsedToken.Claims.(*tokenVerifierClaims)
	if !ok {
		return httpcommon.Claims{}, fmt.Errorf("parse token: invalid claims type %T", parsedToken.Claims)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return httpcommon.Claims{}, fmt.Errorf("parse subject: %w", err)
	}

	return httpcommon.Claims{
		UserID: userID,
		Role:   claims.Role,
		Status: claims.Status,
	}, nil
}
