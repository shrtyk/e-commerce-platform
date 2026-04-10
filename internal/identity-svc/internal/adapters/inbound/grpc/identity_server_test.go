package grpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound/mocks"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

const testSessionTTL = 24 * time.Hour

func TestRegisterUser(t *testing.T) {
	tests := []struct {
		name         string
		request      *identityv1.RegisterUserRequest
		setup        func(*testDeps)
		expectedCode codes.Code
		assert       func(*testing.T, *identityv1.RegisterUserResponse)
	}{
		{
			name: "success",
			request: &identityv1.RegisterUserRequest{
				Email:       "user@example.com",
				Password:    "secret",
				DisplayName: "John Doe",
			},
			setup: func(deps *testDeps) {
				userID := uuid.New()
				sessionID := uuid.New()

				deps.hasher.EXPECT().Hash("secret").Return("hashed", nil)
				deps.users.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "John Doe",
					Role:        domain.UserRoleUser,
					Status:      domain.UserStatusActive,
				}, nil)
				deps.sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: sessionID, UserID: userID}, nil)
				deps.tokens.EXPECT().IssueToken(testifymock.Anything).Return("access-token", nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.RegisterUserResponse) {
				require.Equal(t, "access-token", response.GetAccessToken())
				require.NotEmpty(t, response.GetRefreshToken())
				require.Equal(t, "user@example.com", response.GetProfile().GetEmail())
				require.Equal(t, "John Doe", response.GetProfile().GetDisplayName())
				require.Equal(t, identityv1.UserRole_USER_ROLE_USER, response.GetProfile().GetRole())
				require.Equal(t, identityv1.UserStatus_USER_STATUS_ACTIVE, response.GetProfile().GetStatus())
			},
		},
		{
			name: "invalid input",
			request: &identityv1.RegisterUserRequest{
				Email:    "",
				Password: "",
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "email already exists",
			request: &identityv1.RegisterUserRequest{
				Email:    "user@example.com",
				Password: "secret",
			},
			setup: func(deps *testDeps) {
				deps.hasher.EXPECT().Hash("secret").Return("hashed", nil)
				deps.users.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.User{}, outbound.ErrDuplicateEmail)
			},
			expectedCode: codes.AlreadyExists,
		},
		{
			name: "internal service error",
			request: &identityv1.RegisterUserRequest{
				Email:    "user@example.com",
				Password: "secret",
			},
			setup: func(deps *testDeps) {
				deps.hasher.EXPECT().Hash("secret").Return("", errors.New("hash failed"))
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, deps := newTestIdentityServer(t)
			if tt.setup != nil {
				tt.setup(deps)
			}

			response, err := server.RegisterUser(context.Background(), tt.request)
			requireCode(t, err, tt.expectedCode)
			if tt.expectedCode == codes.OK {
				require.NotNil(t, response)
				if tt.assert != nil {
					tt.assert(t, response)
				}
				return
			}

			require.Nil(t, response)
		})
	}
}

func TestLoginUser(t *testing.T) {
	tests := []struct {
		name         string
		request      *identityv1.LoginUserRequest
		setup        func(*testDeps)
		expectedCode codes.Code
		assert       func(*testing.T, *identityv1.LoginUserResponse)
	}{
		{
			name: "success",
			request: &identityv1.LoginUserRequest{
				Email:    "user@example.com",
				Password: "secret",
			},
			setup: func(deps *testDeps) {
				userID := uuid.New()
				sessionID := uuid.New()

				storedUser := domain.User{
					ID:           userID,
					Email:        "user@example.com",
					PasswordHash: "stored-hash",
					DisplayName:  "John",
					Role:         domain.UserRoleAdmin,
					Status:       domain.UserStatusActive,
				}

				deps.users.EXPECT().GetByEmail(testifymock.Anything, "user@example.com").Return(storedUser, nil)
				deps.hasher.EXPECT().Verify("secret", "stored-hash").Return(true, nil)
				deps.sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: sessionID, UserID: userID}, nil)
				deps.tokens.EXPECT().IssueToken(storedUser).Return("access-token", nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.LoginUserResponse) {
				require.Equal(t, "access-token", response.GetAccessToken())
				require.NotEmpty(t, response.GetRefreshToken())
				require.Equal(t, identityv1.UserRole_USER_ROLE_ADMIN, response.GetProfile().GetRole())
				require.Equal(t, identityv1.UserStatus_USER_STATUS_ACTIVE, response.GetProfile().GetStatus())
			},
		},
		{
			name: "invalid credentials",
			request: &identityv1.LoginUserRequest{
				Email:    "",
				Password: "",
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "internal service error",
			request: &identityv1.LoginUserRequest{
				Email:    "user@example.com",
				Password: "secret",
			},
			setup: func(deps *testDeps) {
				deps.users.EXPECT().GetByEmail(testifymock.Anything, "user@example.com").Return(domain.User{}, errors.New("db down"))
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, deps := newTestIdentityServer(t)
			if tt.setup != nil {
				tt.setup(deps)
			}

			response, err := server.LoginUser(context.Background(), tt.request)
			requireCode(t, err, tt.expectedCode)
			if tt.expectedCode == codes.OK {
				require.NotNil(t, response)
				if tt.assert != nil {
					tt.assert(t, response)
				}
				return
			}

			require.Nil(t, response)
		})
	}
}

func TestRefreshToken(t *testing.T) {
	tests := []struct {
		name         string
		request      *identityv1.RefreshTokenRequest
		setup        func(*testDeps)
		expectedCode codes.Code
		assert       func(*testing.T, *identityv1.RefreshTokenResponse)
	}{
		{
			name: "success",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				oldSessionID := uuid.New()
				newSessionID := uuid.New()
				secret := "secret-value"

				deps.refreshTokenRequest = &identityv1.RefreshTokenRequest{
					RefreshToken: fmt.Sprintf("%s.%s", oldSessionID, secret),
				}

				deps.sessions.EXPECT().GetByID(testifymock.Anything, oldSessionID).Return(domain.Session{
					ID:        oldSessionID,
					UserID:    userID,
					TokenHash: hashSessionSecret(secret),
					ExpiresAt: time.Now().UTC().Add(time.Hour),
				}, nil)

				deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{
					ID:     userID,
					Email:  "user@example.com",
					Role:   domain.UserRoleUser,
					Status: domain.UserStatusActive,
				}, nil)

				deps.sessions.EXPECT().Revoke(testifymock.Anything, oldSessionID, testifymock.Anything).Return(nil)
				deps.sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: newSessionID, UserID: userID}, nil)
				deps.tokens.EXPECT().IssueToken(testifymock.Anything).Return("access-token", nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.RefreshTokenResponse) {
				require.Equal(t, "access-token", response.GetAccessToken())
				require.NotEmpty(t, response.GetRefreshToken())
			},
		},
		{
			name: "invalid refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "invalid",
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "internal service error",
			setup: func(deps *testDeps) {
				sessionID := uuid.New()
				deps.refreshTokenRequest = &identityv1.RefreshTokenRequest{RefreshToken: fmt.Sprintf("%s.%s", sessionID, "secret")}
				deps.sessions.EXPECT().GetByID(testifymock.Anything, sessionID).Return(domain.Session{}, errors.New("db down"))
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, deps := newTestIdentityServer(t)
			request := tt.request
			if tt.setup != nil {
				tt.setup(deps)
				if deps.refreshTokenRequest != nil {
					request = deps.refreshTokenRequest
				}
			}

			response, err := server.RefreshToken(context.Background(), request)
			requireCode(t, err, tt.expectedCode)
			if tt.expectedCode == codes.OK {
				require.NotNil(t, response)
				if tt.assert != nil {
					tt.assert(t, response)
				}
				return
			}

			require.Nil(t, response)
		})
	}
}

func TestGetProfile(t *testing.T) {
	tests := []struct {
		name         string
		request      *identityv1.GetProfileRequest
		setup        func(*testDeps)
		expectedCode codes.Code
		assert       func(*testing.T, *identityv1.GetProfileResponse)
	}{
		{
			name: "success",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.getProfileRequest = &identityv1.GetProfileRequest{UserId: userID.String()}
				deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "John",
					Role:        domain.UserRoleAdmin,
					Status:      domain.UserStatusDisabled,
				}, nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.GetProfileResponse) {
				require.Equal(t, "user@example.com", response.GetProfile().GetEmail())
				require.Equal(t, identityv1.UserRole_USER_ROLE_ADMIN, response.GetProfile().GetRole())
				require.Equal(t, identityv1.UserStatus_USER_STATUS_DISABLED, response.GetProfile().GetStatus())
			},
		},
		{
			name: "invalid user id",
			request: &identityv1.GetProfileRequest{
				UserId: "invalid-uuid",
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "user not found",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.getProfileRequest = &identityv1.GetProfileRequest{UserId: userID.String()}
				deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{}, outbound.ErrUserNotFound)
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "internal service error",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.getProfileRequest = &identityv1.GetProfileRequest{UserId: userID.String()}
				deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{}, errors.New("db down"))
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, deps := newTestIdentityServer(t)
			request := tt.request
			if tt.setup != nil {
				tt.setup(deps)
				if deps.getProfileRequest != nil {
					request = deps.getProfileRequest
				}
			}

			response, err := server.GetProfile(context.Background(), request)
			requireCode(t, err, tt.expectedCode)
			if tt.expectedCode == codes.OK {
				require.NotNil(t, response)
				if tt.assert != nil {
					tt.assert(t, response)
				}
				return
			}

			require.Nil(t, response)
		})
	}
}

func TestUpdateProfile(t *testing.T) {
	tests := []struct {
		name         string
		request      *identityv1.UpdateProfileRequest
		setup        func(*testDeps)
		expectedCode codes.Code
		assert       func(*testing.T, *identityv1.UpdateProfileResponse)
	}{
		{
			name: "success",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.updateProfileRequest = &identityv1.UpdateProfileRequest{UserId: userID.String(), DisplayName: "Updated Name"}
				deps.users.EXPECT().Update(
					testifymock.Anything,
					userID,
					outbound.UserUpdateParams{DisplayName: "Updated Name"},
				).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "Updated Name",
					Role:        domain.UserRoleUser,
					Status:      domain.UserStatusActive,
				}, nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.UpdateProfileResponse) {
				require.Equal(t, "Updated Name", response.GetProfile().GetDisplayName())
				require.Equal(t, identityv1.UserRole_USER_ROLE_USER, response.GetProfile().GetRole())
				require.Equal(t, identityv1.UserStatus_USER_STATUS_ACTIVE, response.GetProfile().GetStatus())
			},
		},
		{
			name: "invalid user id",
			request: &identityv1.UpdateProfileRequest{
				UserId:      "invalid-uuid",
				DisplayName: "Name",
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "profile update failed",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.updateProfileRequest = &identityv1.UpdateProfileRequest{UserId: userID.String(), DisplayName: "Name"}
				deps.users.EXPECT().Update(
					testifymock.Anything,
					userID,
					outbound.UserUpdateParams{DisplayName: "Name"},
				).Return(domain.User{}, auth.ErrProfileUpdateFailed)
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "empty display name returns profile",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.updateProfileRequest = &identityv1.UpdateProfileRequest{UserId: userID.String(), DisplayName: ""}
				deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "Current Name",
					Role:        domain.UserRoleUser,
					Status:      domain.UserStatusActive,
				}, nil)
			},
			expectedCode: codes.OK,
			assert: func(t *testing.T, response *identityv1.UpdateProfileResponse) {
				require.Equal(t, "Current Name", response.GetProfile().GetDisplayName())
			},
		},
		{
			name: "user not found",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.updateProfileRequest = &identityv1.UpdateProfileRequest{UserId: userID.String(), DisplayName: "Name"}
				deps.users.EXPECT().Update(
					testifymock.Anything,
					userID,
					outbound.UserUpdateParams{DisplayName: "Name"},
				).Return(domain.User{}, outbound.ErrUserNotFound)
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "internal service error",
			setup: func(deps *testDeps) {
				userID := uuid.New()
				deps.updateProfileRequest = &identityv1.UpdateProfileRequest{UserId: userID.String(), DisplayName: "Name"}
				deps.users.EXPECT().Update(
					testifymock.Anything,
					userID,
					outbound.UserUpdateParams{DisplayName: "Name"},
				).Return(domain.User{}, errors.New("db down"))
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, deps := newTestIdentityServer(t)
			request := tt.request
			if tt.setup != nil {
				tt.setup(deps)
				if deps.updateProfileRequest != nil {
					request = deps.updateProfileRequest
				}
			}

			response, err := server.UpdateProfile(context.Background(), request)
			requireCode(t, err, tt.expectedCode)
			if tt.expectedCode == codes.OK {
				require.NotNil(t, response)
				if tt.assert != nil {
					tt.assert(t, response)
				}
				return
			}

			require.Nil(t, response)
		})
	}
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()

	if code == codes.OK {
		require.NoError(t, err)
		return
	}

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, code, st.Code())
}

func TestGetProfileInternalErrorLogsOriginalError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	server, deps := newTestIdentityServerWithLogger(t, logger)
	userID := uuid.New()
	deps.users.EXPECT().GetByID(testifymock.Anything, userID).Return(domain.User{}, errors.New("db down"))

	response, err := server.GetProfile(context.Background(), &identityv1.GetProfileRequest{UserId: userID.String()})
	require.Nil(t, response)
	requireCode(t, err, codes.Internal)
	require.Contains(t, logs.String(), "grpc internal error")
	require.Contains(t, logs.String(), "get user by id: db down")
}

type testDeps struct {
	users                *outboundmocks.MockUserRepository
	sessions             *outboundmocks.MockSessionRepository
	hasher               *outboundmocks.MockPasswordHasher
	tokens               *outboundmocks.MockTokenIssuer
	refreshTokenRequest  *identityv1.RefreshTokenRequest
	getProfileRequest    *identityv1.GetProfileRequest
	updateProfileRequest *identityv1.UpdateProfileRequest
}

func newTestIdentityServer(t *testing.T) (*IdentityServer, *testDeps) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newTestIdentityServerWithLogger(t, logger)
}

func newTestIdentityServerWithLogger(t *testing.T, logger *slog.Logger) (*IdentityServer, *testDeps) {
	t.Helper()

	users := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	tokens := outboundmocks.NewMockTokenIssuer(t)

	txProvider := &stubProvider{
		repos: auth.IdentityRepos{
			Users:    users,
			Sessions: sessions,
		},
	}

	authService := auth.NewAuthService(users, sessions, txProvider, hasher, tokens, testSessionTTL)

	return NewIdentityServer(authService, logger), &testDeps{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		tokens:   tokens,
	}
}

type stubProvider struct {
	repos auth.IdentityRepos
}

func (p *stubProvider) WithTransaction(
	_ context.Context,
	_ *sql.TxOptions,
	fn func(uow tx.UnitOfWork[auth.IdentityRepos]) error,
) error {
	return fn(stubUnitOfWork{repos: p.repos})
}

type stubUnitOfWork struct {
	repos auth.IdentityRepos
}

func (u stubUnitOfWork) Repos() auth.IdentityRepos {
	return u.repos
}

func hashSessionSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
