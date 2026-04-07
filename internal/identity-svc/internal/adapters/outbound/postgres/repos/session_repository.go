package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

type SessionRepository struct {
	queries sqlc.Querier
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return NewSessionRepositoryFromQuerier(sqlc.New(db))
}

func NewSessionRepositoryFromQuerier(queries sqlc.Querier) *SessionRepository {
	return &SessionRepository{queries: queries}
}

func (r *SessionRepository) Create(ctx context.Context, session domain.Session) (domain.Session, error) {
	userID, err := uuid.Parse(session.UserID)
	if err != nil {
		return domain.Session{}, fmt.Errorf("parse user id: %w", err)
	}

	result, err := r.queries.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    userID,
		TokenHash: session.TokenHash,
		ExpiresAt: session.ExpiresAt,
	})
	if err != nil {
		return domain.Session{}, fmt.Errorf("create session: %w", err)
	}

	return mapSession(result), nil
}

func (r *SessionRepository) GetByID(ctx context.Context, sessionID string) (domain.Session, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return domain.Session{}, fmt.Errorf("parse session id: %w", err)
	}

	result, err := r.queries.GetSessionByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Session{}, outbound.ErrSessionNotFound
		}

		return domain.Session{}, fmt.Errorf("get session by id %q: %w", sessionID, err)
	}

	session := mapSession(result)
	return session, nil
}

func mapSession(session sqlc.Session) domain.Session {
	result := domain.Session{
		ID:        session.SessionID.String(),
		UserID:    session.UserID.String(),
		TokenHash: session.TokenHash,
		ExpiresAt: session.ExpiresAt,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
	if session.RevokedAt.Valid {
		revokedAt := session.RevokedAt.Time
		result.RevokedAt = &revokedAt
	}

	return result
}
