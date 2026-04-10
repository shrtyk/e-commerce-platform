package transport

import "github.com/google/uuid"

type Claims struct {
	UserID uuid.UUID
	Role   string
	Status string
}
