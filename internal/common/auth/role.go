package auth

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleUnknown Role = ""
	RoleUser    Role = "user"
	RoleAdmin   Role = "admin"
)

func (r Role) IsValid() bool {
	switch r {
	case RoleUser, RoleAdmin:
		return true
	default:
		return false
	}
}

func ParseRole(raw string) (Role, error) {
	role := Role(strings.ToLower(strings.TrimSpace(raw)))
	if !role.IsValid() {
		return RoleUnknown, fmt.Errorf("parse role %q: %w", raw, ErrInvalidRole)
	}

	return role, nil
}

func HasAnyRole(role Role, allowed ...Role) bool {
	if !role.IsValid() {
		return false
	}

	if len(allowed) == 0 {
		return true
	}

	for _, current := range allowed {
		if role == current {
			return true
		}
	}

	return false
}
