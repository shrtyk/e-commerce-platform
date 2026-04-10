package auth

import "errors"

var (
	ErrInvalidRole   = errors.New("auth invalid role")
	ErrInvalidStatus = errors.New("auth invalid status")
	ErrInvalidClaims = errors.New("auth invalid claims")
)
