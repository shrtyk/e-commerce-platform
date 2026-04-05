package domain

import "time"

type UserStatus string
type UserRole string

const (
	UserStatusUnknown  UserStatus = ""
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

const (
	UserRoleUnknown UserRole = ""
	UserRoleUser    UserRole = "user"
	UserRoleAdmin   UserRole = "admin"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Role         UserRole
	Status       UserStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
