package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

var ErrProfileUpdateFailed = errors.New("identity profile update failed")

type GetProfileResult struct {
	UserID      string
	Email       string
	DisplayName *string
	Role        domain.UserRole
	Status      domain.UserStatus
}

type UpdateProfileInput struct {
	DisplayName *string
}

type UpdateProfileResult = GetProfileResult

func (s *AuthService) GetMyProfile(ctx context.Context, userID uuid.UUID) (GetProfileResult, error) {
	user, err := s.repos.Users.GetByID(ctx, userID)
	if errors.Is(err, outbound.ErrUserNotFound) {
		return GetProfileResult{}, outbound.ErrUserNotFound
	}
	if err != nil {
		return GetProfileResult{}, fmt.Errorf("get user by id: %w", err)
	}

	return toProfileResult(user), nil
}

func (s *AuthService) UpdateMyProfile(
	ctx context.Context,
	userID uuid.UUID,
	input UpdateProfileInput,
) (UpdateProfileResult, error) {
	if input.DisplayName == nil {
		return s.GetMyProfile(ctx, userID)
	}

	params := outbound.UserUpdateParams{DisplayName: *input.DisplayName}

	updatedUser, err := s.repos.Users.Update(ctx, userID, params)
	if errors.Is(err, outbound.ErrUserNotFound) {
		return UpdateProfileResult{}, outbound.ErrUserNotFound
	}
	if err != nil {
		return UpdateProfileResult{}, fmt.Errorf("update user by id: %w", err)
	}

	return toProfileResult(updatedUser), nil
}

func toProfileResult(user domain.User) GetProfileResult {
	return GetProfileResult{
		UserID:      user.ID.String(),
		Email:       user.Email,
		DisplayName: optionalDisplayName(user.DisplayName),
		Role:        user.Role,
		Status:      user.Status,
	}
}

func optionalDisplayName(displayName string) *string {
	if displayName == "" {
		return nil
	}

	value := displayName
	return &value
}
