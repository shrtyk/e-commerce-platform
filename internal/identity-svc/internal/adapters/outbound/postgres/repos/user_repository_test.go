package repos

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

var userColumns = []string{"user_id", "email", "password_hash", "display_name", "status", "created_at", "updated_at"}

func TestUserRepositoryCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	status := string(domain.UserStatusActive)
	user := domain.User{
		Email:        "user@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	rows := sqlmock.NewRows(
		userColumns,
	).AddRow(
		userID,
		user.Email,
		user.PasswordHash,
		user.DisplayName,
		status,
		user.CreatedAt,
		user.UpdatedAt,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO users (
			email,
			password_hash,
			display_name,
			status
		) VALUES ($1, $2, $3, $4)
		RETURNING user_id, email, password_hash, display_name, status, created_at, updated_at
	`)).
		WithArgs(
			user.Email,
			user.PasswordHash,
			user.DisplayName,
			string(user.Status),
		).
		WillReturnRows(rows)

	createdUser, err := repo.Create(context.Background(), user)
	require.NoError(t, err)
	require.Equal(t, userID.String(), createdUser.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepositoryGetByEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	status := string(domain.UserStatusActive)
	email := "user@example.com"
	displayName := "Test User"

	rows := sqlmock.NewRows(
		userColumns,
	).AddRow(
		userID,
		email,
		"hash",
		displayName,
		status,
		now,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT user_id, email, password_hash, display_name, status, created_at, updated_at
		FROM users
		WHERE email = $1
		LIMIT 1
	`)).
		WithArgs(email).
		WillReturnRows(rows)

	user, err := repo.GetByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, userID.String(), user.ID)
	require.Equal(t, domain.UserStatusActive, user.Status)
	require.Equal(t, displayName, user.DisplayName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepositoryGetByEmailNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)
	email := "missing@example.com"

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT user_id, email, password_hash, display_name, status, created_at, updated_at
		FROM users
		WHERE email = $1
		LIMIT 1
	`)).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows(
			userColumns,
		))

	user, err := repo.GetByEmail(context.Background(), email)
	require.ErrorIs(t, err, outbound.ErrUserNotFound)
	require.Nil(t, user)
	require.NoError(t, mock.ExpectationsWereMet())
}
