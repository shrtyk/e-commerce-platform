//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/require"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestRegisterUserCreatesUser(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
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
	testhelper.CleanupDB(t, testhelper.TestDB)
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
	testhelper.CleanupDB(t, testhelper.TestDB)
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
	err := testhelper.TestDB.QueryRow("SELECT revoked_at IS NOT NULL FROM sessions WHERE session_id = $1", sessionID).Scan(&revoked)
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
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
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
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

type registerHTTPInput struct {
	Email       string
	Password    string
	DisplayName *string
}

func newAuthStack(t *testing.T) *testhelper.TestStack {
	t.Helper()

	return testhelper.NewTestStack(t, testhelper.TestDB)
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
