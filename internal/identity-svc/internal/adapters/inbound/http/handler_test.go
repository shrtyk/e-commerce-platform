package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound/mocks"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRegisterUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setup      func(*authFixture)
		statusCode int
		assertBody func(t *testing.T, body string)
	}{
		{
			name: "success",
			body: `{"email":"user@example.com","password":"super-secret"}`,
			setup: func(f *authFixture) {
				userID := uuid.New()
				sessionID := uuid.New()

				f.hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				f.users.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.User{ID: userID, Email: "user@example.com", Status: domain.UserStatusActive, Role: domain.UserRoleUser}, nil)
				f.sessions.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
						return domain.Session{ID: sessionID, UserID: session.UserID, TokenHash: session.TokenHash, ExpiresAt: session.ExpiresAt}, nil
					})
				f.tokens.EXPECT().IssueToken(testifymock.Anything).Return("access-token", nil)
			},
			statusCode: http.StatusCreated,
			assertBody: func(t *testing.T, body string) {
				var response dto.AuthTokensResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "access-token", response.AccessToken)
				require.NotEmpty(t, response.RefreshToken)
			},
		},
		{
			name:       "bad request",
			body:       `{"email":`,
			setup:      func(_ *authFixture) {},
			statusCode: http.StatusBadRequest,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_request", response.Code)
			},
		},
		{
			name: "service error",
			body: `{"email":"user@example.com","password":"super-secret"}`,
			setup: func(f *authFixture) {
				f.hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				f.users.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.User{}, outbound.ErrDuplicateEmail)
			},
			statusCode: http.StatusConflict,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "email_already_registered", response.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAuthFixture(t)
			tt.setup(fixture)

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service", fixture.service, nil, noop.NewTracerProvider().Tracer("test-service"))
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
		})
	}
}

func TestRegisterAdmin(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		tokenVerifier *stubTokenVerifier
		setup         func(*authFixture)
		statusCode    int
		assertBody    func(t *testing.T, body string)
	}{
		{
			name:       "unauthorized without token",
			statusCode: http.StatusUnauthorized,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "unauthorized", response.Code)
			},
		},
		{
			name:       "forbidden for non-admin",
			authHeader: "Bearer user-token",
			tokenVerifier: &stubTokenVerifier{
				claims: transport.Claims{UserID: uuid.New(), Role: string(domain.UserRoleUser), Status: string(domain.UserStatusActive)},
			},
			statusCode: http.StatusForbidden,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "forbidden", response.Code)
			},
		},
		{
			name:       "success for admin",
			authHeader: "Bearer admin-token",
			tokenVerifier: &stubTokenVerifier{
				claims: transport.Claims{UserID: uuid.New(), Role: string(domain.UserRoleAdmin), Status: string(domain.UserStatusActive)},
			},
			setup: func(f *authFixture) {
				adminID := uuid.New()
				sessionID := uuid.New()

				f.hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				f.users.EXPECT().
					Create(testifymock.Anything, testifymock.MatchedBy(func(user domain.User) bool {
						return user.Role == domain.UserRoleAdmin
					})).
					Return(domain.User{ID: adminID, Email: "admin@example.com", Status: domain.UserStatusActive, Role: domain.UserRoleAdmin}, nil)
				f.sessions.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
						return domain.Session{ID: sessionID, UserID: session.UserID, TokenHash: session.TokenHash, ExpiresAt: session.ExpiresAt}, nil
					})
				f.tokens.EXPECT().IssueToken(testifymock.Anything).Return("access-token", nil)
			},
			statusCode: http.StatusCreated,
			assertBody: func(t *testing.T, body string) {
				var response dto.AuthTokensResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "access-token", response.AccessToken)
				require.NotEmpty(t, response.RefreshToken)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAuthFixture(t)
			if tt.setup != nil {
				tt.setup(fixture)
			}

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service", fixture.service, tt.tokenVerifier, noop.NewTracerProvider().Tracer("test-service"))
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/register-admin", strings.NewReader(`{"email":"admin@example.com","password":"super-secret"}`))
			req.Header.Set("Content-Type", "application/json")
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
		})
	}
}

func TestLoginUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setup      func(*authFixture)
		statusCode int
		assertBody func(t *testing.T, body string)
	}{
		{
			name: "success",
			body: `{"email":"user@example.com","password":"super-secret"}`,
			setup: func(f *authFixture) {
				userID := uuid.New()
				sessionID := uuid.New()

				f.users.EXPECT().
					GetByEmail(testifymock.Anything, "user@example.com").
					Return(domain.User{ID: userID, Email: "user@example.com", PasswordHash: "stored-hash", Status: domain.UserStatusActive, Role: domain.UserRoleUser}, nil)
				f.hasher.EXPECT().Verify("super-secret", "stored-hash").Return(true, nil)
				f.sessions.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
						return domain.Session{ID: sessionID, UserID: session.UserID, TokenHash: session.TokenHash, ExpiresAt: session.ExpiresAt}, nil
					})
				f.tokens.EXPECT().IssueToken(testifymock.Anything).Return("access-token", nil)
			},
			statusCode: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				var response dto.AuthTokensResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "access-token", response.AccessToken)
				require.NotEmpty(t, response.RefreshToken)
			},
		},
		{
			name:       "bad request",
			body:       `{"email":`,
			setup:      func(_ *authFixture) {},
			statusCode: http.StatusBadRequest,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_request", response.Code)
			},
		},
		{
			name: "service error",
			body: `{"email":"user@example.com","password":"super-secret"}`,
			setup: func(f *authFixture) {
				f.users.EXPECT().
					GetByEmail(testifymock.Anything, "user@example.com").
					Return(domain.User{}, outbound.ErrUserNotFound)
			},
			statusCode: http.StatusUnauthorized,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_credentials", response.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAuthFixture(t)
			tt.setup(fixture)

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service", fixture.service, nil, noop.NewTracerProvider().Tracer("test-service"))
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
		})
	}
}

func TestRefreshToken(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setup      func(*authFixture)
		statusCode int
		assertBody func(t *testing.T, body string)
	}{
		{
			name: "success",
			body: `{"refreshToken":"11111111-1111-1111-1111-111111111111.refresh-secret"}`,
			setup: func(f *authFixture) {
				userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
				sessionID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
				nextSessionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
				secret := "refresh-secret"

				f.sessions.EXPECT().
					GetByID(testifymock.Anything, sessionID).
					Return(domain.Session{ID: sessionID, UserID: userID, TokenHash: testHashSessionSecret(secret), ExpiresAt: time.Now().UTC().Add(time.Hour)}, nil)
				f.users.EXPECT().
					GetByID(testifymock.Anything, userID).
					Return(domain.User{ID: userID, Email: "user@example.com", Status: domain.UserStatusActive, Role: domain.UserRoleUser}, nil)
				f.sessions.EXPECT().
					Revoke(testifymock.Anything, sessionID, testifymock.Anything).
					Return(nil)
				f.sessions.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
						return domain.Session{ID: nextSessionID, UserID: session.UserID, TokenHash: session.TokenHash, ExpiresAt: session.ExpiresAt}, nil
					})
				f.tokens.EXPECT().IssueToken(testifymock.Anything).Return("new-access-token", nil)
			},
			statusCode: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				var response dto.AuthTokensResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "new-access-token", response.AccessToken)
				require.NotEmpty(t, response.RefreshToken)
			},
		},
		{
			name:       "bad request",
			body:       `{"refreshToken":`,
			setup:      func(_ *authFixture) {},
			statusCode: http.StatusBadRequest,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_request", response.Code)
			},
		},
		{
			name: "service error",
			body: `{"refreshToken":"invalid"}`,
			setup: func(_ *authFixture) {
			},
			statusCode: http.StatusUnauthorized,
			assertBody: func(t *testing.T, body string) {
				var response dto.ErrorResponse
				require.NoError(t, json.Unmarshal([]byte(body), &response))
				require.Equal(t, "invalid_refresh_token", response.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newAuthFixture(t)
			tt.setup(fixture)

			h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service", fixture.service, nil, noop.NewTracerProvider().Tracer("test-service"))
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			require.Equal(t, tt.statusCode, res.Code)
			tt.assertBody(t, res.Body.String())
		})
	}
}

func TestHandlerRoutes(t *testing.T) {
	fixture := newAuthFixture(t)
	h := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "test-service", fixture.service, nil, noop.NewTracerProvider().Tracer("test-service"))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	h.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, "ok", res.Body.String())
}

func TestHandleOpenAPIErrorReturnsSafeJSON(t *testing.T) {
	h := NewIdentityHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/register", nil)
	res := httptest.NewRecorder()

	h.HandleOpenAPIError(res, req, assertAnError{})

	require.Equal(t, http.StatusBadRequest, res.Code)

	var response dto.ErrorResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, "invalid_request", response.Code)
	require.Equal(t, "invalid request parameters", response.Message)
}

func TestOpenAPIValidationErrorThroughRouterReturnsSafeJSONEnvelope(t *testing.T) {
	fixture := newAuthFixture(t)

	originalRegisterOpenAPIRoutes := registerOpenAPIRoutes
	t.Cleanup(func() {
		registerOpenAPIRoutes = originalRegisterOpenAPIRoutes
	})

	seamCalled := false
	registerOpenAPIRoutes = func(_ dto.ServerInterface, options dto.ChiServerOptions) http.Handler {
		seamCalled = true
		require.NotNil(t, options.ErrorHandlerFunc)

		req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(`{"email":`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()

		options.ErrorHandlerFunc(res, req, assertAnError{})

		require.Equal(t, http.StatusBadRequest, res.Code)

		var response dto.ErrorResponse
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response))
		require.Equal(t, "invalid_request", response.Code)
		require.Equal(t, "invalid request parameters", response.Message)
		require.NotContains(t, res.Body.String(), "must not leak")

		return options.BaseRouter
	}

	_ = NewRouter(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		"test-service",
		fixture.service,
		nil,
		noop.NewTracerProvider().Tracer("test-service"),
	)

	require.True(t, seamCalled)
	fixture.users.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	fixture.users.AssertNotCalled(t, "GetByEmail", testifymock.Anything, testifymock.Anything)
	fixture.users.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	fixture.sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	fixture.sessions.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	fixture.sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	fixture.hasher.AssertNotCalled(t, "Hash", testifymock.Anything)
	fixture.hasher.AssertNotCalled(t, "Verify", testifymock.Anything, testifymock.Anything)
	fixture.tokens.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

type assertAnError struct{}

func (assertAnError) Error() string { return "must not leak" }

type authFixture struct {
	users    *outboundmocks.MockUserRepository
	sessions *outboundmocks.MockSessionRepository
	hasher   *outboundmocks.MockPasswordHasher
	tokens   *outboundmocks.MockTokenIssuer
	service  *auth.AuthService
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	users := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	tokens := outboundmocks.NewMockTokenIssuer(t)

	provider := testTxProvider{repos: auth.IdentityRepos{Users: users, Sessions: sessions}}
	service := auth.NewAuthService(users, sessions, provider, hasher, tokens, time.Hour)

	return &authFixture{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		tokens:   tokens,
		service:  service,
	}
}

type testTxProvider struct {
	repos auth.IdentityRepos
}

func (p testTxProvider) WithTransaction(
	ctx context.Context,
	_ *sql.TxOptions,
	fn func(uow tx.UnitOfWork[auth.IdentityRepos]) error,
) error {
	return fn(testUnitOfWork{repos: p.repos})
}

type testUnitOfWork struct {
	repos auth.IdentityRepos
}

type stubTokenVerifier struct {
	claims transport.Claims
	err    error
}

func (s *stubTokenVerifier) Verify(_ string) (transport.Claims, error) {
	if s.err != nil {
		return transport.Claims{}, s.err
	}

	return s.claims, nil
}

func (u testUnitOfWork) Repos() auth.IdentityRepos {
	return u.repos
}

func testHashSessionSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
