package domain

import "time"

type UserStatus string

const (
	UserStatusUnknown  UserStatus = ""
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Status       UserStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
