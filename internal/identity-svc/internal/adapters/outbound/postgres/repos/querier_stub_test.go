package repos

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
)

type stubQuerier struct {
	createUserFunc     func(ctx context.Context, arg sqlc.CreateUserParams) (sqlc.User, error)
	getUserByEmailFunc func(ctx context.Context, email string) (sqlc.User, error)
	createSessionFunc  func(ctx context.Context, arg sqlc.CreateSessionParams) (sqlc.Session, error)
	getSessionByIDFunc func(ctx context.Context, sessionID uuid.UUID) (sqlc.Session, error)
}

func (s stubQuerier) CreateUser(ctx context.Context, arg sqlc.CreateUserParams) (sqlc.User, error) {
	if s.createUserFunc == nil {
		return sqlc.User{}, fmt.Errorf("unexpected CreateUser call")
	}

	return s.createUserFunc(ctx, arg)
}

func (s stubQuerier) GetUserByEmail(ctx context.Context, email string) (sqlc.User, error) {
	if s.getUserByEmailFunc == nil {
		return sqlc.User{}, fmt.Errorf("unexpected GetUserByEmail call")
	}

	return s.getUserByEmailFunc(ctx, email)
}

func (s stubQuerier) CreateSession(ctx context.Context, arg sqlc.CreateSessionParams) (sqlc.Session, error) {
	if s.createSessionFunc == nil {
		return sqlc.Session{}, fmt.Errorf("unexpected CreateSession call")
	}

	return s.createSessionFunc(ctx, arg)
}

func (s stubQuerier) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (sqlc.Session, error) {
	if s.getSessionByIDFunc == nil {
		return sqlc.Session{}, fmt.Errorf("unexpected GetSessionByID call")
	}

	return s.getSessionByIDFunc(ctx, sessionID)
}
