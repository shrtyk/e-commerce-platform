package repos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	postgresqlc "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/sqlc"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	portoutbound "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

type UserRepository struct {
	queries *postgresqlc.Queries
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{queries: postgresqlc.New(db)}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User) error {
	err := r.queries.CreateUser(ctx, postgresqlc.CreateUserParams{
		UserID:       user.ID,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		DisplayName: sql.NullString{
			String: user.DisplayName,
			Valid:  user.DisplayName != "",
		},
		Status:    string(user.Status),
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	result, err := r.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, portoutbound.ErrUserNotFound
		}

		return nil, fmt.Errorf("get user by email %q: %w", email, err)
	}

	user := domain.User{
		ID:           result.UserID,
		Email:        result.Email,
		PasswordHash: result.PasswordHash,
		Status:       domain.UserStatus(result.Status),
		CreatedAt:    result.CreatedAt,
		UpdatedAt:    result.UpdatedAt,
	}
	if result.DisplayName.Valid {
		user.DisplayName = result.DisplayName.String
	}

	return &user, nil
}
