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

	"github.com/stretchr/testify/require"

	identityv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/identity/v1"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/testhelper"
)

func TestGetMyProfileRequiresAuth(t *testing.T) {
	stack := newAuthStack(t)

	request := httptest.NewRequest(http.MethodGet, "/v1/profile/me", nil)
	response := httptest.NewRecorder()

	stack.HTTPHandler.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnauthorized, response.Code)
}

func TestGetMyProfileReturnsUserProfile(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	grpcRegisterResponse, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:       "profile-read@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: "Profile Reader",
	})
	require.NoError(t, err)

	httpProfile := getMyProfileHTTP(t, stack.HTTPHandler, grpcRegisterResponse.GetAccessToken(), http.StatusOK)
	require.Equal(t, "profile-read@example.com", string(httpProfile.Email))
	require.Equal(t, "Profile Reader", valueOrEmpty(httpProfile.DisplayName))

	grpcProfileResponse, err := grpcClient.GetProfile(
		grpcAuthContext(t, grpcRegisterResponse.GetAccessToken()),
		&identityv1.GetProfileRequest{UserId: grpcRegisterResponse.GetProfile().GetUserId()},
	)
	require.NoError(t, err)
	require.Equal(t, httpProfile.UserId, grpcProfileResponse.GetProfile().GetUserId())
	require.Equal(t, string(httpProfile.Email), grpcProfileResponse.GetProfile().GetEmail())
}

func TestUpdateMyProfileUpdatesDisplayName(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	registerResponse, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:       "profile-update@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: "Before Update",
	})
	require.NoError(t, err)

	updatedProfile := updateMyProfileHTTP(t, stack.HTTPHandler, registerResponse.GetAccessToken(), strPtr("After Update"), http.StatusOK)
	require.Equal(t, "After Update", valueOrEmpty(updatedProfile.DisplayName))

	grpcProfileResponse, err := grpcClient.GetProfile(
		grpcAuthContext(t, registerResponse.GetAccessToken()),
		&identityv1.GetProfileRequest{UserId: registerResponse.GetProfile().GetUserId()},
	)
	require.NoError(t, err)
	require.Equal(t, "After Update", grpcProfileResponse.GetProfile().GetDisplayName())
}

func TestUpdateMyProfileWithNilDisplayName(t *testing.T) {
	testhelper.CleanupDB(t, testhelper.TestDB)
	stack := newAuthStack(t)
	grpcClient := identityv1.NewIdentityServiceClient(stack.GRPCConn)

	registerResponse, err := grpcClient.RegisterUser(context.Background(), &identityv1.RegisterUserRequest{
		Email:       "profile-nil@example.com",
		Password:    testhelper.TestPassword,
		DisplayName: "Initial Name",
	})
	require.NoError(t, err)

	updatedProfile := updateMyProfileHTTP(t, stack.HTTPHandler, registerResponse.GetAccessToken(), nil, http.StatusOK)
	require.Equal(t, "Initial Name", valueOrEmpty(updatedProfile.DisplayName))

	grpcProfileResponse, err := grpcClient.GetProfile(
		grpcAuthContext(t, registerResponse.GetAccessToken()),
		&identityv1.GetProfileRequest{UserId: registerResponse.GetProfile().GetUserId()},
	)
	require.NoError(t, err)
	require.Equal(t, "Initial Name", grpcProfileResponse.GetProfile().GetDisplayName())
}

func getMyProfileHTTP(t *testing.T, handler http.Handler, accessToken string, expectedStatus int) dto.UserProfile {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/profile/me", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	if expectedStatus != http.StatusOK {
		return dto.UserProfile{}
	}

	var profile dto.UserProfile
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profile))

	return profile
}

func updateMyProfileHTTP(
	t *testing.T,
	handler http.Handler,
	accessToken string,
	displayName *string,
	expectedStatus int,
) dto.UserProfile {
	t.Helper()

	body, err := json.Marshal(dto.UpdateProfileRequest{DisplayName: displayName})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPatch, "/v1/profile/me", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	require.Equal(t, expectedStatus, response.Code)

	if expectedStatus != http.StatusOK {
		return dto.UserProfile{}
	}

	var profile dto.UserProfile
	require.NoError(t, json.NewDecoder(response.Body).Decode(&profile))

	return profile
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
