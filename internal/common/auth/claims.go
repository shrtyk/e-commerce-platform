package auth

import (
	"fmt"

	"github.com/google/uuid"
)

type Claims struct {
	UserID uuid.UUID
	Role   Role
	Status Status
}

func (c Claims) Validate() error {
	if c.UserID == uuid.Nil {
		return fmt.Errorf("user id is required: %w", ErrInvalidClaims)
	}

	if !c.Role.IsValid() {
		return fmt.Errorf("role is invalid: %w", ErrInvalidClaims)
	}

	if !c.Status.IsValid() {
		return fmt.Errorf("status is invalid: %w", ErrInvalidClaims)
	}

	return nil
}

func (c Claims) CanAccess(allowedRoles ...Role) bool {
	if err := c.Validate(); err != nil {
		return false
	}

	if !c.Status.CanAuthenticate() {
		return false
	}

	return HasAnyRole(c.Role, allowedRoles...)
}
