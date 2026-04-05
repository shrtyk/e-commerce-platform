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

func TestUserRepositoryCreate(t *testing.T) {
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	user := domain.User{
		Email:        "user@example.com",
		PasswordHash: "hash",
		DisplayName:  "Test User",
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	repo := &UserRepository{queries: stubQuerier{
		createUserFunc: func(_ context.Context, arg sqlc.CreateUserParams) (sqlc.CreateUserRow, error) {
			require.Equal(t, user.Email, arg.Email)
			require.Equal(t, user.PasswordHash, arg.PasswordHash)
			require.Equal(t, user.DisplayName, arg.DisplayName.String)
			require.True(t, arg.DisplayName.Valid)
			require.Equal(t, string(user.Role), arg.RoleCode)
			require.Equal(t, string(user.Status), arg.Status)

			return sqlc.CreateUserRow{
				UserID:       userID,
				Email:        user.Email,
				PasswordHash: user.PasswordHash,
				DisplayName:  sql.NullString{String: user.DisplayName, Valid: true},
				RoleCode:     string(user.Role),
				Status:       string(user.Status),
				CreatedAt:    user.CreatedAt,
				UpdatedAt:    user.UpdatedAt,
			}, nil
		},
	}}

	createdUser, err := repo.Create(context.Background(), user)
	require.NoError(t, err)
	require.Equal(t, userID.String(), createdUser.ID)
	require.Equal(t, domain.UserRoleUser, createdUser.Role)
}

func TestUserRepositoryGetByEmail(t *testing.T) {
	now := time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	email := "user@example.com"
	displayName := "Test User"
	repo := &UserRepository{queries: stubQuerier{
		getUserByEmailFunc: func(_ context.Context, gotEmail string) (sqlc.GetUserByEmailRow, error) {
			require.Equal(t, email, gotEmail)

			return sqlc.GetUserByEmailRow{
				UserID:       userID,
				Email:        email,
				PasswordHash: "hash",
				DisplayName:  sql.NullString{String: displayName, Valid: true},
				RoleCode:     string(domain.UserRoleAdmin),
				Status:       string(domain.UserStatusActive),
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
	}}

	user, err := repo.GetByEmail(context.Background(), email)
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, userID.String(), user.ID)
	require.Equal(t, domain.UserRoleAdmin, user.Role)
	require.Equal(t, domain.UserStatusActive, user.Status)
	require.Equal(t, displayName, user.DisplayName)
}

func TestUserRepositoryGetByEmailNotFound(t *testing.T) {
	email := "missing@example.com"
	repo := &UserRepository{queries: stubQuerier{
		getUserByEmailFunc: func(_ context.Context, gotEmail string) (sqlc.GetUserByEmailRow, error) {
			require.Equal(t, email, gotEmail)
			return sqlc.GetUserByEmailRow{}, sql.ErrNoRows
		},
	}}

	user, err := repo.GetByEmail(context.Background(), email)
	require.ErrorIs(t, err, outbound.ErrUserNotFound)
	require.Nil(t, user)
}
