package grpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/google/uuid"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound/mocks"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

func TestServerAuthInterceptor(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, h *grpcHarness)
		call          func(t *testing.T, h *grpcHarness) error
		expectedCode  codes.Code
		wantVerifyHit bool
	}{
		{
			name: "public method bypass auth",
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				_, err := h.client.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{})
				return err
			},
			expectedCode:  codes.InvalidArgument,
			wantVerifyHit: false,
		},
		{
			name: "public login bypass auth",
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				_, err := h.client.LoginUser(context.Background(), &identityv1.LoginUserRequest{})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "public refresh token bypass auth",
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				_, err := h.client.RefreshToken(context.Background(), &identityv1.RefreshTokenRequest{})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "protected method requires auth",
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				_, err := h.client.GetProfile(context.Background(), &identityv1.GetProfileRequest{UserId: uuid.NewString()})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "update profile requires auth",
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				_, err := h.client.UpdateProfile(context.Background(), &identityv1.UpdateProfileRequest{
					UserId:      uuid.NewString(),
					DisplayName: "Updated",
				})
				return err
			},
			expectedCode:  codes.Unauthenticated,
			wantVerifyHit: false,
		},
		{
			name: "valid auth for protected method passes",
			setup: func(t *testing.T, h *grpcHarness) {
				t.Helper()

				userID := uuid.New()
				h.requestedUserID = userID
				h.claims = transport.Claims{UserID: userID, Role: "user", Status: "active"}
				h.deps.users.EXPECT().GetByID(testifymock.MatchedBy(func(ctx context.Context) bool {
					claims, ok := transport.ClaimsFromContext(ctx)
					if !ok {
						return false
					}

					return claims.UserID == userID && claims.Role == "user" && claims.Status == "active"
				}), userID).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "John",
					Role:        domain.UserRoleUser,
					Status:      domain.UserStatusActive,
				}, nil)
			},
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer valid-token")
				response, err := h.client.GetProfile(ctx, &identityv1.GetProfileRequest{UserId: h.requestedUserID.String()})
				if err != nil {
					return err
				}

				require.Equal(t, "user@example.com", response.GetProfile().GetEmail())
				return nil
			},
			expectedCode:  codes.OK,
			wantVerifyHit: true,
		},
		{
			name: "valid auth for update profile passes claims in context",
			setup: func(t *testing.T, h *grpcHarness) {
				t.Helper()

				userID := uuid.New()
				h.requestedUserID = userID
				h.claims = transport.Claims{UserID: userID, Role: "user", Status: "active"}
				h.deps.users.EXPECT().Update(
					testifymock.MatchedBy(func(ctx context.Context) bool {
						claims, ok := transport.ClaimsFromContext(ctx)
						if !ok {
							return false
						}

						return claims.UserID == userID && claims.Role == "user" && claims.Status == "active"
					}),
					userID,
					testifymock.MatchedBy(func(params interface{}) bool {
						updateParams, ok := params.(outbound.UserUpdateParams)
						if !ok {
							return false
						}

						return updateParams.DisplayName == "Updated"
					}),
				).Return(domain.User{
					ID:          userID,
					Email:       "user@example.com",
					DisplayName: "Updated",
					Role:        domain.UserRoleUser,
					Status:      domain.UserStatusActive,
				}, nil)
			},
			call: func(t *testing.T, h *grpcHarness) error {
				t.Helper()

				ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer valid-token")
				response, err := h.client.UpdateProfile(ctx, &identityv1.UpdateProfileRequest{
					UserId:      h.requestedUserID.String(),
					DisplayName: "Updated",
				})
				if err != nil {
					return err
				}

				require.Equal(t, "user@example.com", response.GetProfile().GetEmail())
				require.Equal(t, "Updated", response.GetProfile().GetDisplayName())
				return nil
			},
			expectedCode:  codes.OK,
			wantVerifyHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newGRPCHarness(t)
			if tt.setup != nil {
				tt.setup(t, h)
			}

			err := tt.call(t, h)
			if tt.expectedCode == codes.OK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tt.expectedCode, status.Code(err))
			}

			require.Equal(t, tt.wantVerifyHit, h.verifier.called)
		})
	}
}

type grpcHarness struct {
	client          identityv1.IdentityServiceClient
	verifier        *testTokenVerifier
	deps            *testDeps
	claims          transport.Claims
	requestedUserID uuid.UUID
}

func newGRPCHarness(t *testing.T) *grpcHarness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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

	verifier := &testTokenVerifier{}
	server := NewServer(logger, "identity-svc-test", authService, verifier)

	listener := bufconn.Listen(1024 * 1024)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpcpkg.NewClient(
		"passthrough:///identity-svc-test",
		grpcpkg.WithContextDialer(dialer),
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
	})

	h := &grpcHarness{
		client:          identityv1.NewIdentityServiceClient(conn),
		verifier:        verifier,
		requestedUserID: uuid.New(),
		deps: &testDeps{
			users:    users,
			sessions: sessions,
			hasher:   hasher,
			tokens:   tokens,
		},
	}

	verifier.verify = func(token string) (transport.Claims, error) {
		return h.claims, nil
	}

	return h
}

type testTokenVerifier struct {
	verify func(token string) (transport.Claims, error)
	called bool
}

var _ httpcommon.TokenVerifier = (*testTokenVerifier)(nil)

func (v *testTokenVerifier) Verify(token string) (transport.Claims, error) {
	v.called = true
	if v.verify == nil {
		return transport.Claims{}, nil
	}

	return v.verify(token)
}
