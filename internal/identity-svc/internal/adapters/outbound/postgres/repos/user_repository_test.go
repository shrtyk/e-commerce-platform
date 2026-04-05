package repos

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	portoutbound "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

func TestUserRepositoryCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)
	user := domain.User{
		ID:           "user-1",
		Email:        "user@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO users (
			user_id,
			email,
			password_hash,
			display_name,
			status,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`)).
		WithArgs(
			user.ID,
			user.Email,
			user.PasswordHash,
			user.DisplayName,
			string(user.Status),
			user.CreatedAt,
			user.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.Create(context.Background(), user)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepositoryGetByEmail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows(
		[]string{"user_id", "email", "password_hash", "display_name", "status", "created_at", "updated_at"},
	).AddRow(
		"user-1",
		"user@example.com",
		"hash",
		"Test User",
		"active",
		now,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT user_id, email, password_hash, display_name, status, created_at, updated_at
		FROM users
		WHERE email = $1
		LIMIT 1
	`)).
		WithArgs("user@example.com").
		WillReturnRows(rows)

	user, err := repo.GetByEmail(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, domain.UserStatusActive, user.Status)
	require.Equal(t, "Test User", user.DisplayName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserRepositoryReturnsDomainNotFoundError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewUserRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT user_id, email, password_hash, display_name, status, created_at, updated_at
		FROM users
		WHERE email = $1
		LIMIT 1
	`)).
		WithArgs("missing@example.com").
		WillReturnRows(sqlmock.NewRows(
			[]string{"user_id", "email", "password_hash", "display_name", "status", "created_at", "updated_at"},
		))

	user, err := repo.GetByEmail(context.Background(), "missing@example.com")
	require.ErrorIs(t, err, portoutbound.ErrUserNotFound)
	require.Nil(t, user)
	require.NoError(t, mock.ExpectationsWereMet())
}
