package transport

import (
	"fmt"

	"github.com/shrtyk/e-commerce-platform/internal/common/auth"
)

func ToAuthClaims(claims Claims) (auth.Claims, error) {
	parsedRole, err := auth.ParseRole(claims.Role)
	if err != nil {
		return auth.Claims{}, fmt.Errorf("parse role from transport claims: %w", err)
	}

	parsedStatus, err := auth.ParseStatus(claims.Status)
	if err != nil {
		return auth.Claims{}, fmt.Errorf("parse status from transport claims: %w", err)
	}

	authClaims := auth.Claims{
		UserID: claims.UserID,
		Role:   parsedRole,
		Status: parsedStatus,
	}

	if err := authClaims.Validate(); err != nil {
		return auth.Claims{}, fmt.Errorf("validate transport claims: %w", err)
	}

	return authClaims, nil
}

func FromAuthClaims(claims auth.Claims) (Claims, error) {
	if err := claims.Validate(); err != nil {
		return Claims{}, fmt.Errorf("validate auth claims: %w", err)
	}

	return Claims{
		UserID: claims.UserID,
		Role:   string(claims.Role),
		Status: string(claims.Status),
	}, nil
}
