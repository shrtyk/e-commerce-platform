package grpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

type IdentityServer struct {
	identityv1.UnimplementedIdentityServiceServer

	authService *auth.AuthService
	logger      *slog.Logger
}

func NewIdentityServer(authService *auth.AuthService, logger *slog.Logger) *IdentityServer {
	if logger == nil {
		logger = slog.Default()
	}

	return &IdentityServer{authService: authService, logger: logger}
}

func (s *IdentityServer) RegisterUser(
	ctx context.Context,
	req *identityv1.RegisterUserRequest,
) (*identityv1.RegisterUserResponse, error) {
	result, err := s.authService.RegisterUser(ctx, toRegisterUserInput(req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toRegisterUserResponse(result), nil
}

func (s *IdentityServer) LoginUser(
	ctx context.Context,
	req *identityv1.LoginUserRequest,
) (*identityv1.LoginUserResponse, error) {
	result, err := s.authService.LoginUser(ctx, toLoginUserInput(req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toLoginUserResponse(result), nil
}

func (s *IdentityServer) RefreshToken(
	ctx context.Context,
	req *identityv1.RefreshTokenRequest,
) (*identityv1.RefreshTokenResponse, error) {
	result, err := s.authService.RefreshToken(ctx, toRefreshTokenInput(req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toRefreshTokenResponse(result), nil
}

func (s *IdentityServer) GetProfile(
	ctx context.Context,
	req *identityv1.GetProfileRequest,
) (*identityv1.GetProfileResponse, error) {
	userID, err := toUserID(req.GetUserId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid user id")
	}

	result, err := s.authService.GetMyProfile(ctx, userID)
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toGetProfileResponse(result), nil
}

func (s *IdentityServer) UpdateProfile(
	ctx context.Context,
	req *identityv1.UpdateProfileRequest,
) (*identityv1.UpdateProfileResponse, error) {
	userID, err := toUserID(req.GetUserId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid user id")
	}

	result, err := s.authService.UpdateMyProfile(ctx, userID, toUpdateProfileInput(req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toUpdateProfileResponse(result), nil
}

func (s *IdentityServer) mapServiceError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidRegisterInput):
		return status.Errorf(codes.InvalidArgument, "invalid register input")
	case errors.Is(err, auth.ErrEmailAlreadyRegistered):
		return status.Errorf(codes.AlreadyExists, "email already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return status.Errorf(codes.Unauthenticated, "invalid credentials")
	case errors.Is(err, auth.ErrInvalidRefreshToken):
		return status.Errorf(codes.Unauthenticated, "invalid refresh token")
	case errors.Is(err, outbound.ErrUserNotFound):
		return status.Errorf(codes.NotFound, "user not found")
	case errors.Is(err, auth.ErrProfileUpdateFailed):
		return status.Errorf(codes.InvalidArgument, "invalid profile input")
	default:
		s.logger.Error("grpc internal error", "error", err.Error())
		return status.Errorf(codes.Internal, "internal error")
	}
}
