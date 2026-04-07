package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

type UserRepository struct {
	queries sqlc.Querier
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return NewUserRepositoryFromQuerier(sqlc.New(db))
}

func NewUserRepositoryFromQuerier(queries sqlc.Querier) *UserRepository {
	return &UserRepository{queries: queries}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	result, err := r.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		DisplayName: sql.NullString{
			String: user.DisplayName,
			Valid:  user.DisplayName != "",
		},
		RoleCode: string(user.Role),
		Status:   string(user.Status),
	})
	if err != nil {
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		if ok && pgErr.Code == "23505" {
			return domain.User{}, fmt.Errorf("create user: %w", outbound.ErrDuplicateEmail)
		}
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}

	createdUser := domain.User{
		ID:           result.UserID.String(),
		Email:        result.Email,
		PasswordHash: result.PasswordHash,
		Role:         domain.UserRole(result.RoleCode),
		Status:       domain.UserStatus(result.Status),
		CreatedAt:    result.CreatedAt,
		UpdatedAt:    result.UpdatedAt,
	}
	if result.DisplayName.Valid {
		createdUser.DisplayName = result.DisplayName.String
	}

	return createdUser, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	result, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, outbound.ErrUserNotFound
		}

		return domain.User{}, fmt.Errorf("get user by email %q: %w", email, err)
	}

	user := domain.User{
		ID:           result.UserID.String(),
		Email:        result.Email,
		PasswordHash: result.PasswordHash,
		Role:         domain.UserRole(result.RoleCode),
		Status:       domain.UserStatus(result.Status),
		CreatedAt:    result.CreatedAt,
		UpdatedAt:    result.UpdatedAt,
	}
	if result.DisplayName.Valid {
		user.DisplayName = result.DisplayName.String
	}

	return user, nil
}
