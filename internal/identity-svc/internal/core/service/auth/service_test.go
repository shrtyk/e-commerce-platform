package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound/mocks"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	registeredEmail = "user@example.com"
	strongPassword  = "super-secret"
)

func TestRegisterUserCreatesUser(t *testing.T) {
	userUUID := uuid.New()
	pwd := "pwd"
	pwdHash := "hash"
	normalizedEmail := registeredEmail
	rawEmail := "  USER@Example.com  "
	normalizedName := "John Doe"
	expectedUser := domain.User{
		Email:        normalizedEmail,
		PasswordHash: pwdHash,
		DisplayName:  normalizedName,
		Status:       domain.UserStatusActive,
	}
	repo := outboundmocks.NewMockUserRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	auth := NewAuthService(repo, hasher)
	displayName := "  John Doe  "

	repo.EXPECT().
		GetByEmail(testifymock.Anything, normalizedEmail).
		Return(nil, outbound.ErrUserNotFound)
	hasher.EXPECT().
		Hash(pwd).
		Return(pwdHash, nil)
	repo.EXPECT().
		Create(testifymock.Anything, expectedUser).
		Return(domain.User{
			ID:           userUUID.String(),
			Email:        normalizedEmail,
			PasswordHash: pwdHash,
			DisplayName:  normalizedName,
			Status:       domain.UserStatusActive,
		}, nil)

	result, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:       rawEmail,
		Password:    pwd,
		DisplayName: &displayName,
	})

	require.NoError(t, err)
	require.Equal(t, userUUID.String(), result.ID)
	require.Equal(t, normalizedEmail, result.Email)
	require.Equal(t, normalizedName, result.DisplayName)
	require.Equal(t, domain.UserStatusActive, result.Status)
}

func TestRegisterUserRejectsDuplicateEmail(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	auth := NewAuthService(repo, hasher)

	repo.EXPECT().
		GetByEmail(testifymock.Anything, registeredEmail).
		Return(&domain.User{ID: "existing-user"}, nil)

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorIs(t, err, ErrEmailAlreadyRegistered)
	hasher.AssertNotCalled(t, "Hash", testifymock.Anything)
	repo.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
}

func TestRegisterUserHashError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	auth := NewAuthService(repo, hasher)

	repo.EXPECT().
		GetByEmail(testifymock.Anything, registeredEmail).
		Return(nil, outbound.ErrUserNotFound)
	hasher.EXPECT().
		Hash(strongPassword).
		Return("", errors.New("hash failed"))

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "hash password")
	repo.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
}

func TestRegisterUserRepoError(t *testing.T) {
	tests := []struct {
		name         string
		lookupErr    error
		createErr    error
		expectedText string
	}{
		{
			name:         "lookup",
			lookupErr:    errors.New("db down"),
			expectedText: "lookup user by email",
		},
		{
			name:         "create",
			lookupErr:    outbound.ErrUserNotFound,
			createErr:    errors.New("insert failed"),
			expectedText: "create user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			auth := NewAuthService(repo, hasher)

			repo.EXPECT().
				GetByEmail(testifymock.Anything, registeredEmail).
				Return(nil, tt.lookupErr)

			if tt.lookupErr == outbound.ErrUserNotFound {
				hasher.EXPECT().Hash(strongPassword).Return("hashed-password", nil)
				repo.EXPECT().
					Create(testifymock.Anything, testifymock.Anything).
					Return(domain.User{}, tt.createErr)
			}

			_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
				Email:    registeredEmail,
				Password: strongPassword,
			})

			require.ErrorContains(t, err, tt.expectedText)
		})
	}
}

func TestLoginUserReturnsUser(t *testing.T) {
	userUUID := uuid.New()
	normalizedEmail := registeredEmail
	repo := outboundmocks.NewMockUserRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	auth := NewAuthService(repo, hasher)
	storedUser := &domain.User{
		ID:           userUUID.String(),
		Email:        normalizedEmail,
		PasswordHash: "stored-hash",
		DisplayName:  "John Doe",
		Status:       domain.UserStatusActive,
	}

	repo.EXPECT().
		GetByEmail(testifymock.Anything, normalizedEmail).
		Return(storedUser, nil)
	hasher.EXPECT().
		Verify(strongPassword, storedUser.PasswordHash).
		Return(true, nil)

	result, err := auth.LoginUser(context.Background(), LoginUserInput{
		Email:    "  USER@Example.com  ",
		Password: strongPassword,
	})

	require.NoError(t, err)
	require.Equal(t, storedUser.ID, result.ID)
	require.Equal(t, normalizedEmail, result.Email)
	require.Equal(t, storedUser.DisplayName, result.DisplayName)
	require.Equal(t, storedUser.Status, result.Status)
}

func TestLoginUserRejectsInvalidCredentials(t *testing.T) {
	tests := []struct {
		name      string
		user      *domain.User
		lookupErr error
		verified  bool
	}{
		{
			name:      "missing user",
			lookupErr: outbound.ErrUserNotFound,
		},
		{
			name: "bad password",
			user: &domain.User{
				ID:           uuid.NewString(),
				Email:        registeredEmail,
				PasswordHash: "stored-hash",
				Status:       domain.UserStatusActive,
			},
			verified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			auth := NewAuthService(repo, hasher)

			repo.EXPECT().
				GetByEmail(testifymock.Anything, registeredEmail).
				Return(tt.user, tt.lookupErr)

			if tt.user != nil {
				hasher.EXPECT().Verify(strongPassword, tt.user.PasswordHash).Return(tt.verified, nil)
			}

			_, err := auth.LoginUser(context.Background(), LoginUserInput{
				Email:    registeredEmail,
				Password: strongPassword,
			})

			require.ErrorIs(t, err, ErrInvalidCredentials)
		})
	}
}

func TestLoginUserVerifyError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	auth := NewAuthService(repo, hasher)
	storedUser := &domain.User{
		ID:           uuid.NewString(),
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Status:       domain.UserStatusActive,
	}

	repo.EXPECT().
		GetByEmail(testifymock.Anything, registeredEmail).
		Return(storedUser, nil)
	hasher.EXPECT().
		Verify(strongPassword, storedUser.PasswordHash).
		Return(false, errors.New("bcrypt down"))

	_, err := auth.LoginUser(context.Background(), LoginUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "verify password")
}
