//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestRegisterInvalidInput(t *testing.T) {
	stack := newAuthStack(t)

	body, err := json.Marshal(map[string]any{"email": "", "password": testhelper.TestPassword})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestLoginEmptyFields(t *testing.T) {
	stack := newAuthStack(t)

	body, err := json.Marshal(map[string]any{"email": "", "password": ""})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestRefreshInvalidToken(t *testing.T) {
	stack := newAuthStack(t)

	refreshHTTP(t, stack.HTTPHandler, testhelper.InvalidToken, http.StatusUnauthorized)
}

func TestRefreshRevokedToken(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)

	tokens := registerHTTP(t, stack.HTTPHandler, registerHTTPInput{
		Email:    "refresh-revoked@example.com",
		Password: testhelper.TestPassword,
	})

	refreshHTTP(t, stack.HTTPHandler, tokens.RefreshToken, http.StatusOK)
	refreshHTTP(t, stack.HTTPHandler, tokens.RefreshToken, http.StatusUnauthorized)
}

func TestGRPCRegisterDuplicateEmail(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	_, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:    "grpc-duplicate@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	_, err = grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:    "grpc-duplicate@example.com",
		Password: testhelper.TestPassword,
	})
	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestGRPCLoginInvalidCredentials(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	_, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:    "grpc-invalid-login@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	_, err = grpcClient.LoginUser(context.Background(), &identityv1.LoginUserRequest{
		Email:    "grpc-invalid-login@example.com",
		Password: "wrong-password",
	})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestGRPCGetProfileNotFound(t *testing.T) {
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	registerResponse, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:    "grpc-get-profile-not-found@example.com",
		Password: testhelper.TestPassword,
	})
	require.NoError(t, err)

	_, err = grpcClient.GetProfile(
		grpcAuthContext(t, registerResponse.GetAccessToken()),
		&identityv1.GetProfileRequest{UserId: uuid.NewString()},
	)
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
}
