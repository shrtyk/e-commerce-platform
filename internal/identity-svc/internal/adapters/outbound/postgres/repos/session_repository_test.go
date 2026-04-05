package repos

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

func TestSessionRepositoryCreate(t *testing.T) {
	now := time.Date(2026, time.April, 5, 18, 30, 0, 0, time.UTC)
	sessionID := uuid.New()
	userID := uuid.New()
	expiresAt := now.Add(24 * time.Hour)
	session := domain.Session{
		UserID:    userID.String(),
		TokenHash: "token-hash",
		ExpiresAt: expiresAt,
	}
	repo := &SessionRepository{queries: stubQuerier{
		createSessionFunc: func(_ context.Context, arg sqlc.CreateSessionParams) (sqlc.Session, error) {
			require.Equal(t, userID, arg.UserID)
			require.Equal(t, session.TokenHash, arg.TokenHash)
			require.Equal(t, expiresAt, arg.ExpiresAt)

			return sqlc.Session{
				SessionID: sessionID,
				UserID:    userID,
				TokenHash: session.TokenHash,
				ExpiresAt: expiresAt,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		},
	}}

	createdSession, err := repo.Create(context.Background(), session)
	require.NoError(t, err)
	require.Equal(t, sessionID.String(), createdSession.ID)
	require.Equal(t, session.UserID, createdSession.UserID)
	require.Equal(t, session.TokenHash, createdSession.TokenHash)
}

func TestSessionRepositoryGetByID(t *testing.T) {
	now := time.Date(2026, time.April, 5, 18, 30, 0, 0, time.UTC)
	sessionID := uuid.New()
	userID := uuid.New()
	expiresAt := now.Add(24 * time.Hour)
	repo := &SessionRepository{queries: stubQuerier{
		getSessionByIDFunc: func(_ context.Context, gotID uuid.UUID) (sqlc.Session, error) {
			require.Equal(t, sessionID, gotID)

			return sqlc.Session{
				SessionID: sessionID,
				UserID:    userID,
				TokenHash: "token-hash",
				ExpiresAt: expiresAt,
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		},
	}}

	session, err := repo.GetByID(context.Background(), sessionID.String())
	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, sessionID.String(), session.ID)
	require.Equal(t, userID.String(), session.UserID)
}

func TestSessionRepositoryGetByIDNotFound(t *testing.T) {
	sessionID := uuid.NewString()
	repo := &SessionRepository{queries: stubQuerier{
		getSessionByIDFunc: func(_ context.Context, gotID uuid.UUID) (sqlc.Session, error) {
			require.Equal(t, sessionID, gotID.String())
			return sqlc.Session{}, sql.ErrNoRows
		},
	}}

	session, err := repo.GetByID(context.Background(), sessionID)
	require.ErrorIs(t, err, outbound.ErrSessionNotFound)
	require.Nil(t, session)
}
