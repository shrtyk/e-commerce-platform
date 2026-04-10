package auth

import (
	"fmt"
	"strings"
)

type Status string

const (
	StatusUnknown  Status = ""
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusDisabled:
		return true
	default:
		return false
	}
}

func ParseStatus(raw string) (Status, error) {
	status := Status(strings.ToLower(strings.TrimSpace(raw)))
	if !status.IsValid() {
		return StatusUnknown, fmt.Errorf("parse status %q: %w", raw, ErrInvalidStatus)
	}

	return status, nil
}

func (s Status) CanAuthenticate() bool {
	return s == StatusActive
}
