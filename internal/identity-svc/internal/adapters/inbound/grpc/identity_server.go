package grpc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

const identityServiceName = "identity-svc"

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
		return nil, s.mapServiceError(
			ctx,
			"RegisterUser",
			identityv1.IdentityService_RegisterUser_FullMethodName,
			err,
		)
	}

	return toRegisterUserResponse(result), nil
}

func (s *IdentityServer) LoginUser(
	ctx context.Context,
	req *identityv1.LoginUserRequest,
) (*identityv1.LoginUserResponse, error) {
	result, err := s.authService.LoginUser(ctx, toLoginUserInput(req))
	if err != nil {
		return nil, s.mapServiceError(
			ctx,
			"LoginUser",
			identityv1.IdentityService_LoginUser_FullMethodName,
			err,
		)
	}

	return toLoginUserResponse(result), nil
}

func (s *IdentityServer) RefreshToken(
	ctx context.Context,
	req *identityv1.RefreshTokenRequest,
) (*identityv1.RefreshTokenResponse, error) {
	result, err := s.authService.RefreshToken(ctx, toRefreshTokenInput(req))
	if err != nil {
		return nil, s.mapServiceError(
			ctx,
			"RefreshToken",
			identityv1.IdentityService_RefreshToken_FullMethodName,
			err,
		)
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
		return nil, s.mapServiceError(
			ctx,
			"GetProfile",
			identityv1.IdentityService_GetProfile_FullMethodName,
			err,
			"user_id",
			userID.String(),
		)
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
		return nil, s.mapServiceError(
			ctx,
			"UpdateProfile",
			identityv1.IdentityService_UpdateProfile_FullMethodName,
			err,
			"user_id",
			userID.String(),
		)
	}

	return toUpdateProfileResponse(result), nil
}

func (s *IdentityServer) mapServiceError(ctx context.Context, method, path string, err error, businessFields ...any) error {
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
		logFields := []any{
			slog.String(logging.FieldService, identityServiceName),
			slog.String(logging.FieldRequestID, transport.RequestIDFromContext(ctx)),
			slog.String(logging.FieldTraceID, logging.TraceIDFromContext(ctx)),
			slog.String(logging.FieldMethod, method),
			slog.String(logging.FieldPath, path),
			slog.Int(logging.FieldStatus, int(codes.Internal)),
			slog.String(logging.FieldGRPCStatus, codes.Internal.String()),
			slog.String("error", err.Error()),
		}

		logFields = append(logFields, businessFields...)

		s.logger.Error("grpc internal error", logFields...)
		return status.Errorf(codes.Internal, "internal error")
	}
}
