//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/password/bcrypt"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestRegisterUserCreatesUser(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	httpResponse := registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:       "register-http@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: strPtr("HTTP User"),
	})
	require.NotEmpty(t, httpResponse.AccessToken)
	require.NotEmpty(t, httpResponse.RefreshToken)

	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)
	grpcResponse, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:       "register-grpc@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: "gRPC User",
	})
	require.NoError(t, err)
	require.NotEmpty(t, grpcResponse.GetAccessToken())
	require.NotEmpty(t, grpcResponse.GetRefreshToken())
	require.Equal(t, "register-grpc@example.com", grpcResponse.GetProfile().GetEmail())
}

func TestLoginUserReturnsTokens(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:       "login@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: strPtr("Login User"),
	})

	loginTokens := loginHTTP(t, stack.HTTPHandler, "login@example.com", testhelper.TestPassword)
	require.NotEmpty(t, loginTokens.AccessToken)
	require.NotEmpty(t, loginTokens.RefreshToken)

	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)
	grpcResponse, err := grpcClient.LoginUser(context.Background(), &identityv1.LoginUserRequest{
		Email:    "login@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)
	require.NotEmpty(t, grpcResponse.GetAccessToken())
	require.NotEmpty(t, grpcResponse.GetRefreshToken())
}

func TestRefreshTokenRotatesSession(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	tokens := registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:    "refresh@example.com",
		Password: testhelper.TestPassword,
	})

	firstRefresh := refreshHTTP(t, stack.HTTPHandler, tokens.RefreshToken, http.StatusOK)
	require.NotEmpty(t, firstRefresh.AccessToken)
	require.NotEmpty(t, firstRefresh.RefreshToken)
	require.NotEqual(t, tokens.RefreshToken, firstRefresh.RefreshToken)

	refreshHTTP(t, stack.HTTPHandler, tokens.RefreshToken, http.StatusUnauthorized)

	sessionID := parseSessionID(t, tokens.RefreshToken)
	var revoked bool
	err := harness.DB.QueryRow("SELECT revoked_at IS NOT NULL FROM sessions WHERE session_id = $1", sessionID).Scan(&revoked)
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:    "invalid-credentials@example.com",
		Password: testhelper.TestPassword,
	})

	body, err := json.Marshal(dto.LoginRequest{
		Email:    "invalid-credentials@example.com",
		Password: "wrong-password",
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	var unauthorized dto.ErrorResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&unauthorized))
	require.Equal(t, "invalid_credentials", unauthorized.Code)
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:    "duplicate@example.com",
		Password: testhelper.TestPassword,
	})

	body, err := json.Marshal(dto.RegisterRequest{
		Email:    "duplicate@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code)
}

func TestRegisterAdminEndpointAuth(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	body, err := json.Marshal(dto.RegisterRequest{
		Email:    "admin-new@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	unauthorizedCases := []struct {
		name          string
		authorization string
	}{
		{name: "missing token"},
		{name: "invalid token", authorization: "Bearer invalid-token"},
	}

	for _, tc := range unauthorizedCases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/register-admin", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			if tc.authorization != "" {
				request.Header.Set("Authorization", tc.authorization)
			}
			response := httptest.NewRecorder()

			stack.HTTPHandler.ServeHTTP(response, request)

			require.Equal(t, http.StatusUnauthorized, response.Code)
			var unauthorized dto.ErrorResponse
			require.NoError(t, json.NewDecoder(response.Body).Decode(&unauthorized))
			require.Equal(t, "unauthorized", unauthorized.Code)
			require.Equal(t, "unauthorized", unauthorized.Message)
		})
	}

	userTokens := registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:    "regular@example.com",
		Password: testhelper.TestPassword,
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register-admin", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+userTokens.AccessToken)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
	var forbidden dto.ErrorResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&forbidden))
	require.Equal(t, "forbidden", forbidden.Code)
}

func TestRegisterAdminCreatesAdminUser(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	adminAccessToken := createAdminAccessToken(t, harness.DB, stack.HTTPHandler, "bootstrap-admin@example.com")

	body, err := json.Marshal(dto.RegisterRequest{
		Email:    "new-admin@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register-admin", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+adminAccessToken)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)

	var roleCode string
	err = harness.DB.QueryRow("SELECT role_code FROM users WHERE email = $1", "new-admin@example.com").Scan(&roleCode)
	require.NoError(t, err)
	require.Equal(t, "admin", roleCode)
}

type registerHTTPInput struct {
	Email       string
	Password    string
	DisplayName *string
}

func newAuthStack(t *testing.T) *testhelper.TestStack {
	t.Helper()

	return testhelper.NewTestStack(t, testhelper.IntegrationHarness(t).DB)
}

func registerHTTP(t *testing.T, handler http.Handler, input registerHTTPInput) dto.AuthTokensResponse {
	t.Helper()

	body, err := json.Marshal(dto.RegisterRequest{
		Email:       openapi_types.Email(input.Email),
		Password:    input.Password,
		DisplayName: input.DisplayName,
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code)

	var tokens dto.AuthTokensResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&tokens))

	return tokens
}

func loginHTTP(t *testing.T, handler http.Handler, email, password string) dto.AuthTokensResponse {
	t.Helper()

	body, err := json.Marshal(dto.LoginRequest{Email: openapi_types.Email(email), Password: password})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)

	var tokens dto.AuthTokensResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&tokens))

	return tokens
}

func refreshHTTP(t *testing.T, handler http.Handler, refreshToken string, expectedStatus int) dto.AuthTokensResponse {
	t.Helper()

	body, err := json.Marshal(dto.RefreshTokenRequest{RefreshToken: refreshToken})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	if expectedStatus != http.StatusOK {
		return dto.AuthTokensResponse{}
	}

	var tokens dto.AuthTokensResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&tokens))

	return tokens
}

func parseSessionID(t *testing.T, token string) uuid.UUID {
	t.Helper()

	parts := strings.Split(token, ".")
	require.Len(t, parts, 2)

	sessionID, err := uuid.Parse(parts[0])
	require.NoError(t, err)

	return sessionID
}

func strPtr(value string) *string {
	return &value
}

func createAdminAccessToken(t *testing.T, db *sql.DB, handler http.Handler, email string) string {
	t.Helper()

	hasher := bcrypt.NewHasher(0)
	hash, err := hasher.Hash(testhelper.TestPassword)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO users (email, password_hash, role_code, status) VALUES ($1, $2, 'admin', 'active')`,
		email,
		hash,
	)
	require.NoError(t, err)

	tokens := loginHTTP(t, handler, email, testhelper.TestPassword)
	return tokens.AccessToken
}

func grpcAuthContext(t *testing.T, accessToken string) context.Context {
	t.Helper()
	require.NotEmpty(t, accessToken)

	return metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+accessToken),
	)
}
