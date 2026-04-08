package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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

func NewSessionRepositoryFromTx(tx *sql.Tx) *SessionRepository {
	return NewSessionRepositoryFromQuerier(sqlc.New(tx))
}

func (r *SessionRepository) Create(ctx context.Context, session domain.Session) (domain.Session, error) {
	result, err := r.queries.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    session.UserID,
		TokenHash: session.TokenHash,
		ExpiresAt: session.ExpiresAt,
	})
	if err != nil {
		return domain.Session{}, fmt.Errorf("create session: %w", err)
	}

	return mapSession(result), nil
}

func (r *SessionRepository) GetByID(ctx context.Context, sessionID uuid.UUID) (domain.Session, error) {
	result, err := r.queries.GetSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Session{}, outbound.ErrSessionNotFound
		}

		return domain.Session{}, fmt.Errorf("get session by id %q: %w", sessionID.String(), err)
	}

	session := mapSession(result)
	return session, nil
}

func (r *SessionRepository) Revoke(ctx context.Context, sessionID uuid.UUID, revokedAt time.Time) error {
	affectedRows, err := r.queries.RevokeSession(ctx, sqlc.RevokeSessionParams{
		SessionID: sessionID,
		RevokedAt: sql.NullTime{Time: revokedAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("revoke session %q: %w", sessionID.String(), err)
	}

	if affectedRows == 0 {
		return outbound.ErrSessionNotFound
	}

	return nil
}

func mapSession(session sqlc.Session) domain.Session {
	result := domain.Session{
		ID:        session.SessionID,
		UserID:    session.UserID,
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
