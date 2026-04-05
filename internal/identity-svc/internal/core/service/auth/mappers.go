package auth

import "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"

func toRegisterUserResult(user domain.User) RegisterUserResult {
	return RegisterUserResult{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
