package grpc

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

func toRegisterUserInput(req *identityv1.RegisterUserRequest) auth.RegisterUserInput {
	input := auth.RegisterUserInput{}
	if req == nil {
		return input
	}

	input.Email = req.GetEmail()
	input.Password = req.GetPassword()

	if displayName := req.GetDisplayName(); displayName != "" {
		input.DisplayName = &displayName
	}

	return input
}

func toLoginUserInput(req *identityv1.LoginUserRequest) auth.LoginUserInput {
	if req == nil {
		return auth.LoginUserInput{}
	}

	return auth.LoginUserInput{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	}
}

func toRefreshTokenInput(req *identityv1.RefreshTokenRequest) auth.RefreshTokenInput {
	if req == nil {
		return auth.RefreshTokenInput{}
	}

	return auth.RefreshTokenInput{RefreshToken: req.GetRefreshToken()}
}

func toUpdateProfileInput(req *identityv1.UpdateProfileRequest) auth.UpdateProfileInput {
	if req == nil {
		return auth.UpdateProfileInput{}
	}

	displayName := req.GetDisplayName()
	if displayName == "" {
		return auth.UpdateProfileInput{}
	}

	return auth.UpdateProfileInput{DisplayName: &displayName}
}

func toUserID(raw string) (uuid.UUID, error) {
	userID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse user id: %w", err)
	}

	if userID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("parse user id: user id must not be zero value")
	}

	return userID, nil
}

func toRegisterUserResponse(result auth.RegisterUserResult) *identityv1.RegisterUserResponse {
	return &identityv1.RegisterUserResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Profile:      toUserProfile(result.ID, result.Email, result.DisplayName, result.Role, result.Status),
	}
}

func toLoginUserResponse(result auth.LoginUserResult) *identityv1.LoginUserResponse {
	return &identityv1.LoginUserResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Profile:      toUserProfile(result.ID, result.Email, result.DisplayName, result.Role, result.Status),
	}
}

func toRefreshTokenResponse(result auth.RefreshTokenResult) *identityv1.RefreshTokenResponse {
	return &identityv1.RefreshTokenResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
	}
}

func toGetProfileResponse(result auth.GetProfileResult) *identityv1.GetProfileResponse {
	displayName := ""
	if result.DisplayName != nil {
		displayName = *result.DisplayName
	}

	return &identityv1.GetProfileResponse{
		Profile: toUserProfile(result.UserID, result.Email, displayName, result.Role, result.Status),
	}
}

func toUpdateProfileResponse(result auth.UpdateProfileResult) *identityv1.UpdateProfileResponse {
	displayName := ""
	if result.DisplayName != nil {
		displayName = *result.DisplayName
	}

	return &identityv1.UpdateProfileResponse{
		Profile: toUserProfile(result.UserID, result.Email, displayName, result.Role, result.Status),
	}
}

func toUserProfile(
	userID string,
	email string,
	displayName string,
	role domain.UserRole,
	status domain.UserStatus,
) *identityv1.UserProfile {
	return &identityv1.UserProfile{
		UserId:      userID,
		Email:       email,
		DisplayName: displayName,
		Role:        toUserRole(role),
		Status:      toUserStatus(status),
	}
}

func toUserRole(role domain.UserRole) identityv1.UserRole {
	switch role {
	case domain.UserRoleUser:
		return identityv1.UserRole_USER_ROLE_USER
	case domain.UserRoleAdmin:
		return identityv1.UserRole_USER_ROLE_ADMIN
	default:
		return identityv1.UserRole_USER_ROLE_UNSPECIFIED
	}
}

func toUserStatus(status domain.UserStatus) identityv1.UserStatus {
	switch status {
	case domain.UserStatusActive:
		return identityv1.UserStatus_USER_STATUS_ACTIVE
	case domain.UserStatusDisabled:
		return identityv1.UserStatus_USER_STATUS_DISABLED
	default:
		return identityv1.UserStatus_USER_STATUS_UNSPECIFIED
	}
}
