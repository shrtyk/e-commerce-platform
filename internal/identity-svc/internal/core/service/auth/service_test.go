package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
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

var testSessionTTL = 24 * time.Hour

func TestRegisterUserCreatesUser(t *testing.T) {
	accessToken := "access-token"
	userUUID := uuid.New()
	pwd := "password"
	pwdHash := "hash"
	normalizedEmail := registeredEmail
	rawEmail := "  USER@Example.com  "
	normalizedName := "John Doe"
	expectedUser := domain.User{
		Email:        normalizedEmail,
		PasswordHash: pwdHash,
		DisplayName:  normalizedName,
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
	}
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	displayName := "  John Doe  "
	sessionID := uuid.New()

	hasher.EXPECT().
		Hash(pwd).
		Return(pwdHash, nil)
	repo.EXPECT().
		Create(testifymock.Anything, expectedUser).
		Return(domain.User{
			ID:           userUUID,
			Email:        normalizedEmail,
			PasswordHash: pwdHash,
			DisplayName:  normalizedName,
			Role:         domain.UserRoleUser,
			Status:       domain.UserStatusActive,
		}, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
			require.Equal(t, userUUID, session.UserID)
			require.NotEmpty(t, session.TokenHash)
			require.WithinDuration(t, time.Now().UTC().Add(testSessionTTL), session.ExpiresAt, time.Second)

			return domain.Session{
				ID:        sessionID,
				UserID:    session.UserID,
				TokenHash: session.TokenHash,
				ExpiresAt: session.ExpiresAt,
			}, nil
		})
	issuer.EXPECT().IssueToken(testifymock.MatchedBy(func(user domain.User) bool {
		return user.ID == userUUID && user.Email == normalizedEmail && user.Role == domain.UserRoleUser
	})).Return(accessToken, nil)

	result, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:       rawEmail,
		Password:    pwd,
		DisplayName: &displayName,
	})

	require.NoError(t, err)
	require.Equal(t, userUUID.String(), result.ID)
	require.Equal(t, normalizedEmail, result.Email)
	require.Equal(t, normalizedName, result.DisplayName)
	require.Equal(t, domain.UserRoleUser, result.Role)
	require.Equal(t, domain.UserStatusActive, result.Status)
	require.Equal(t, accessToken, result.AccessToken)
	require.True(t, strings.HasPrefix(result.RefreshToken, sessionID.String()+"."))
}

func TestRegisterUserRejectsDuplicateEmail(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().
		Hash(strongPassword).
		Return("hash", nil)
	repo.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.User{}, outbound.ErrDuplicateEmail)

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorIs(t, err, ErrEmailAlreadyRegistered)
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterUserPasswordLengthPolicy(t *testing.T) {
	tests := []struct {
		name               string
		password           string
		expectInvalidInput bool
	}{
		{
			name:               "too short",
			password:           strings.Repeat("a", 7),
			expectInvalidInput: true,
		},
		{
			name:               "below configured min",
			password:           strings.Repeat("a", 11),
			expectInvalidInput: true,
		},
		{
			name:     "valid min",
			password: strings.Repeat("a", 12),
		},
		{
			name:     "valid max",
			password: strings.Repeat("a", 72),
		},
		{
			name:               "too long",
			password:           strings.Repeat("a", 73),
			expectInvalidInput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL, PasswordPolicy{MinLength: 12})

			if !tt.expectInvalidInput {
				hasher.EXPECT().Hash(tt.password).Return("", errors.New("hash failed"))
			}

			_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
				Email:    registeredEmail,
				Password: tt.password,
			})

			if tt.expectInvalidInput {
				require.ErrorIs(t, err, ErrInvalidRegisterInput)
				repo.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
				issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
				return
			}

			require.ErrorContains(t, err, "hash password")
			repo.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
			issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
		})
	}
}

func TestRegisterUserHashError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().
		Hash(strongPassword).
		Return("", errors.New("hash failed"))

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "hash password")
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterUserRepoError(t *testing.T) {
	tests := []struct {
		name         string
		createErr    error
		expectedText string
	}{
		{
			name:         "create",
			createErr:    errors.New("insert failed"),
			expectedText: "create user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			hasher.EXPECT().Hash(strongPassword).Return("hashed-password", nil)
			repo.EXPECT().
				Create(testifymock.Anything, testifymock.Anything).
				Return(domain.User{}, tt.createErr)

			_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
				Email:    registeredEmail,
				Password: strongPassword,
			})

			require.ErrorContains(t, err, tt.expectedText)
			issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
		})
	}
}

func TestRegisterUserSessionError(t *testing.T) {
	userUUID := uuid.New()
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)
	repo.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.User{ID: userUUID, Email: registeredEmail, Status: domain.UserStatusActive}, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.Session{}, errors.New("session store down"))

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "create session")
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterUserRejectsNilCreatedSessionID(t *testing.T) {
	userUUID := uuid.New()
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)
	repo.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.User{ID: userUUID, Email: registeredEmail, Status: domain.UserStatusActive}, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.Session{ID: uuid.Nil, UserID: userUUID}, nil)

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "create session")
	require.ErrorContains(t, err, "session id is nil")
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterUserRejectsNilCreatedUserID(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)
	repo.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.User{ID: uuid.Nil, Email: registeredEmail, Status: domain.UserStatusActive}, nil)

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "create session")
	require.ErrorContains(t, err, "user id is nil")
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterUserAccessTokenError(t *testing.T) {
	userUUID := uuid.New()
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)
	repo.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.User{ID: userUUID, Email: registeredEmail, Status: domain.UserStatusActive}, nil)
	sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: uuid.New(), UserID: userUUID}, nil)
	issuer.EXPECT().IssueToken(testifymock.Anything).Return("", errors.New("jwt down"))

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{Email: registeredEmail, Password: strongPassword})

	require.ErrorContains(t, err, "issue access token")
}

func TestRegisterUserTxProviderError(t *testing.T) {
	txErr := errors.New("tx store unavailable")
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := &stubProvider{err: txErr}
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)

	_, err := auth.RegisterUser(context.Background(), RegisterUserInput{Email: registeredEmail, Password: strongPassword})

	require.ErrorIs(t, err, txErr)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRegisterAdminCreatesAdmin(t *testing.T) {
	accessToken := "access-token"
	adminUUID := uuid.New()
	pwd := "password"
	pwdHash := "hash"
	normalizedEmail := "admin@example.com"
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	sessionID := uuid.New()

	hasher.EXPECT().Hash(pwd).Return(pwdHash, nil)
	repo.EXPECT().
		Create(testifymock.Anything, testifymock.MatchedBy(func(user domain.User) bool {
			return user.Email == normalizedEmail &&
				user.PasswordHash == pwdHash &&
				user.Role == domain.UserRoleAdmin &&
				user.Status == domain.UserStatusActive
		})).
		Return(domain.User{
			ID:           adminUUID,
			Email:        normalizedEmail,
			PasswordHash: pwdHash,
			Role:         domain.UserRoleAdmin,
			Status:       domain.UserStatusActive,
		}, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
			require.Equal(t, adminUUID, session.UserID)

			return domain.Session{
				ID:        sessionID,
				UserID:    session.UserID,
				TokenHash: session.TokenHash,
				ExpiresAt: session.ExpiresAt,
			}, nil
		})
	issuer.EXPECT().IssueToken(testifymock.MatchedBy(func(user domain.User) bool {
		return user.ID == adminUUID && user.Role == domain.UserRoleAdmin
	})).Return(accessToken, nil)

	result, err := auth.RegisterAdmin(context.Background(), RegisterUserInput{
		Email:    "  ADMIN@Example.com  ",
		Password: pwd,
	})

	require.NoError(t, err)
	require.Equal(t, adminUUID.String(), result.ID)
	require.Equal(t, domain.UserRoleAdmin, result.Role)
	require.Equal(t, accessToken, result.AccessToken)
	require.True(t, strings.HasPrefix(result.RefreshToken, sessionID.String()+"."))
}

func TestEnsureBootstrapAdmin(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*outboundmocks.MockUserRepository, *outboundmocks.MockPasswordHasher)
		expectError string
	}{
		{
			name: "create when missing",
			setup: func(users *outboundmocks.MockUserRepository, hasher *outboundmocks.MockPasswordHasher) {
				hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{}, outbound.ErrUserNotFound)
				users.EXPECT().
					Create(testifymock.Anything, testifymock.MatchedBy(func(user domain.User) bool {
						return user.Email == "admin@example.com" &&
							user.PasswordHash == "hashed-password" &&
							user.Role == domain.UserRoleAdmin &&
							user.Status == domain.UserStatusActive &&
							user.DisplayName == "Bootstrap Admin"
					})).
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}, nil)
			},
		},
		{
			name: "no-op when active admin exists",
			setup: func(users *outboundmocks.MockUserRepository, hasher *outboundmocks.MockPasswordHasher) {
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}, nil)
			},
		},
		{
			name: "fail when existing user not admin-compatible",
			setup: func(users *outboundmocks.MockUserRepository, _ *outboundmocks.MockPasswordHasher) {
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil)
			},
			expectError: "bootstrap admin",
		},
		{
			name: "duplicate then existing active admin is treated as success",
			setup: func(users *outboundmocks.MockUserRepository, hasher *outboundmocks.MockPasswordHasher) {
				hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{}, outbound.ErrUserNotFound).
					Once()
				users.EXPECT().
					Create(testifymock.Anything, testifymock.MatchedBy(func(user domain.User) bool {
						return user.Email == "admin@example.com" && user.Role == domain.UserRoleAdmin && user.Status == domain.UserStatusActive
					})).
					Return(domain.User{}, outbound.ErrDuplicateEmail)
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}, nil).
					Once()
			},
		},
		{
			name: "duplicate then existing incompatible user fails",
			setup: func(users *outboundmocks.MockUserRepository, hasher *outboundmocks.MockPasswordHasher) {
				hasher.EXPECT().Hash("super-secret").Return("hashed-password", nil)
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{}, outbound.ErrUserNotFound).
					Once()
				users.EXPECT().
					Create(testifymock.Anything, testifymock.MatchedBy(func(user domain.User) bool {
						return user.Email == "admin@example.com" && user.Role == domain.UserRoleAdmin && user.Status == domain.UserStatusActive
					})).
					Return(domain.User{}, outbound.ErrDuplicateEmail)
				users.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleUser, Status: domain.UserStatusActive}, nil).
					Once()
			},
			expectError: "bootstrap admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			tt.setup(repo, hasher)

			err := auth.EnsureBootstrapAdmin(context.Background(), BootstrapAdminInput{
				Email:       "admin@example.com",
				Password:    "super-secret",
				DisplayName: stringPtr("Bootstrap Admin"),
			})

			if tt.expectError == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tt.expectError)
		})
	}
}

func TestEnsureBootstrapAdminPasswordLengthPolicy(t *testing.T) {
	tests := []struct {
		name             string
		password         string
		expectInvalid    bool
		expectLookupCall bool
	}{
		{
			name:          "too short",
			password:      strings.Repeat("a", 7),
			expectInvalid: true,
		},
		{
			name:             "valid min",
			password:         strings.Repeat("a", 8),
			expectLookupCall: true,
		},
		{
			name:             "valid max",
			password:         strings.Repeat("a", 72),
			expectLookupCall: true,
		},
		{
			name:          "too long",
			password:      strings.Repeat("a", 73),
			expectInvalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			if tt.expectLookupCall {
				repo.EXPECT().
					GetByEmail(testifymock.Anything, "admin@example.com").
					Return(domain.User{ID: uuid.New(), Email: "admin@example.com", Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}, nil)
			}

			err := auth.EnsureBootstrapAdmin(context.Background(), BootstrapAdminInput{
				Email:    "admin@example.com",
				Password: tt.password,
			})

			if tt.expectInvalid {
				require.ErrorIs(t, err, ErrInvalidRegisterInput)
				repo.AssertNotCalled(t, "GetByEmail", testifymock.Anything, testifymock.Anything)
				hasher.AssertNotCalled(t, "Hash", testifymock.Anything)
				return
			}

			require.NoError(t, err)
			hasher.AssertNotCalled(t, "Hash", testifymock.Anything)
		})
	}
}

func TestLoginUserReturnsUser(t *testing.T) {
	accessToken := "access-token"
	userUUID := uuid.New()
	normalizedEmail := registeredEmail
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
		ID:           userUUID,
		Email:        normalizedEmail,
		PasswordHash: "stored-hash",
		DisplayName:  "John Doe",
		Role:         domain.UserRoleAdmin,
		Status:       domain.UserStatusActive,
	}
	sessionID := uuid.New()

	repo.EXPECT().
		GetByEmail(testifymock.Anything, normalizedEmail).
		Return(storedUser, nil)
	hasher.EXPECT().
		Verify(strongPassword, storedUser.PasswordHash).
		Return(true, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
			require.Equal(t, storedUser.ID, session.UserID)
			require.NotEmpty(t, session.TokenHash)
			require.WithinDuration(t, time.Now().UTC().Add(testSessionTTL), session.ExpiresAt, time.Second)
			return domain.Session{
				ID:        sessionID,
				UserID:    session.UserID,
				TokenHash: session.TokenHash,
				ExpiresAt: session.ExpiresAt,
			}, nil
		})
	issuer.EXPECT().IssueToken(testifymock.MatchedBy(func(user domain.User) bool {
		return user.ID == storedUser.ID && user.Email == normalizedEmail && user.Role == domain.UserRoleAdmin
	})).Return(accessToken, nil)

	result, err := auth.LoginUser(context.Background(), LoginUserInput{
		Email:    "  USER@Example.com  ",
		Password: strongPassword,
	})

	require.NoError(t, err)
	require.Equal(t, storedUser.ID.String(), result.ID)
	require.Equal(t, normalizedEmail, result.Email)
	require.Equal(t, storedUser.DisplayName, result.DisplayName)
	require.Equal(t, domain.UserRoleAdmin, result.Role)
	require.Equal(t, storedUser.Status, result.Status)
	require.Equal(t, accessToken, result.AccessToken)
	require.True(t, strings.HasPrefix(result.RefreshToken, sessionID.String()+"."))
}

func TestLoginUserRejectsInvalidCredentials(t *testing.T) {
	tests := []struct {
		name      string
		user      domain.User
		lookupErr error
		verified  bool
	}{
		{
			name:      "missing user",
			lookupErr: outbound.ErrUserNotFound,
		},
		{
			name: "bad password",
			user: domain.User{
				ID:           uuid.New(),
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
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			repo.EXPECT().
				GetByEmail(testifymock.Anything, registeredEmail).
				Return(tt.user, tt.lookupErr)

			if tt.lookupErr == nil {
				hasher.EXPECT().Verify(strongPassword, tt.user.PasswordHash).Return(tt.verified, nil)
			}

			_, err := auth.LoginUser(context.Background(), LoginUserInput{
				Email:    registeredEmail,
				Password: strongPassword,
			})

			require.ErrorIs(t, err, ErrInvalidCredentials)
			sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
			issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
		})
	}
}

func TestLoginUserPasswordLengthPolicy(t *testing.T) {
	tests := []struct {
		name             string
		password         string
		expectLookupCall bool
	}{
		{
			name:     "too short",
			password: strings.Repeat("a", 7),
		},
		{
			name:             "valid min",
			password:         strings.Repeat("a", 8),
			expectLookupCall: true,
		},
		{
			name:             "valid max",
			password:         strings.Repeat("a", 72),
			expectLookupCall: true,
		},
		{
			name:     "too long",
			password: strings.Repeat("a", 73),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			if tt.expectLookupCall {
				repo.EXPECT().
					GetByEmail(testifymock.Anything, registeredEmail).
					Return(domain.User{}, outbound.ErrUserNotFound)
			}

			_, err := auth.LoginUser(context.Background(), LoginUserInput{
				Email:    registeredEmail,
				Password: tt.password,
			})

			require.ErrorIs(t, err, ErrInvalidCredentials)
			if !tt.expectLookupCall {
				repo.AssertNotCalled(t, "GetByEmail", testifymock.Anything, testifymock.Anything)
			}
			hasher.AssertNotCalled(t, "Verify", testifymock.Anything, testifymock.Anything)
			sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
			issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
		})
	}
}

func TestLoginUserVerifyError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
		ID:           uuid.New(),
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
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestLoginUserSessionError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
		ID:           uuid.New(),
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Status:       domain.UserStatusActive,
	}

	repo.EXPECT().
		GetByEmail(testifymock.Anything, registeredEmail).
		Return(storedUser, nil)
	hasher.EXPECT().
		Verify(strongPassword, storedUser.PasswordHash).
		Return(true, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.Session{}, errors.New("session store down"))

	_, err := auth.LoginUser(context.Background(), LoginUserInput{
		Email:    registeredEmail,
		Password: strongPassword,
	})

	require.ErrorContains(t, err, "create session")
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestLoginUserRejectsNilStoredUserID(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
		ID:           uuid.Nil,
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Status:       domain.UserStatusActive,
	}

	repo.EXPECT().GetByEmail(testifymock.Anything, registeredEmail).Return(storedUser, nil)
	hasher.EXPECT().Verify(strongPassword, storedUser.PasswordHash).Return(true, nil)

	_, err := auth.LoginUser(context.Background(), LoginUserInput{Email: registeredEmail, Password: strongPassword})

	require.ErrorContains(t, err, "create session")
	require.ErrorContains(t, err, "user id is nil")
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestLoginUserAccessTokenError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newRecordingProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)
	storedUser := domain.User{ID: uuid.New(), Email: registeredEmail, PasswordHash: "stored-hash", Status: domain.UserStatusActive}

	repo.EXPECT().GetByEmail(testifymock.Anything, registeredEmail).Return(storedUser, nil)
	hasher.EXPECT().Verify(strongPassword, storedUser.PasswordHash).Return(true, nil)
	sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: uuid.New(), UserID: storedUser.ID}, nil)
	issuer.EXPECT().IssueToken(testifymock.Anything).Return("", errors.New("jwt down"))

	_, err := auth.LoginUser(context.Background(), LoginUserInput{Email: registeredEmail, Password: strongPassword})

	require.ErrorContains(t, err, "issue access token")
	require.False(t, txProvider.committed)
	require.True(t, txProvider.rolledBack)
}

func TestRefreshTokenReturnsRotatedPair(t *testing.T) {
	userID := uuid.New()
	oldSessionID := uuid.New()
	newSessionID := uuid.New()
	secret := "secret-value"
	accessToken := "new-access-token"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	storedUser := domain.User{
		ID:           userID,
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
	}

	sessions.EXPECT().
		GetByID(testifymock.Anything, oldSessionID).
		Return(domain.Session{
			ID:        oldSessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(storedUser, nil)
	sessions.EXPECT().
		Revoke(testifymock.Anything, oldSessionID, testifymock.Anything).
		Return(nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
			require.Equal(t, userID, session.UserID)
			require.NotEmpty(t, session.TokenHash)
			require.WithinDuration(t, time.Now().UTC().Add(testSessionTTL), session.ExpiresAt, time.Second)

			return domain.Session{
				ID:        newSessionID,
				UserID:    session.UserID,
				TokenHash: session.TokenHash,
				ExpiresAt: session.ExpiresAt,
			}, nil
		})
	issuer.EXPECT().IssueToken(storedUser).Return(accessToken, nil)

	result, err := auth.RefreshToken(context.Background(), RefreshTokenInput{
		RefreshToken: formatSessionToken(oldSessionID.String(), secret),
	})

	require.NoError(t, err)
	require.Equal(t, accessToken, result.AccessToken)
	require.True(t, strings.HasPrefix(result.RefreshToken, newSessionID.String()+"."))
}

func TestRefreshTokenRejectsInvalidToken(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: "bad-token"})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsExpiredSession(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(-time.Minute),
		}, nil)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsRevokedSession(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "secret-value"
	revokedAt := time.Now().UTC().Add(-time.Minute)

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
			RevokedAt: &revokedAt,
		}, nil)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsHashMismatch(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret("different-secret"),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), "secret-value")})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsSessionNotFoundOnLookup(t *testing.T) {
	sessionID := uuid.New()

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{}, outbound.ErrSessionNotFound)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), "secret-value")})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsUserNotFound(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(domain.User{}, outbound.ErrUserNotFound)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsSessionNotFoundOnRevoke(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	storedUser := domain.User{
		ID:           userID,
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
	}

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(storedUser, nil)
	sessions.EXPECT().
		Revoke(testifymock.Anything, sessionID, testifymock.Anything).
		Return(outbound.ErrSessionNotFound)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsNilStoredUserID(t *testing.T) {
	sessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    uuid.Nil,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsNilResolvedUserIDForSessionCreation(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        sessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(domain.User{ID: uuid.Nil, Email: registeredEmail, Status: domain.UserStatusActive}, nil)
	sessions.EXPECT().
		Revoke(testifymock.Anything, sessionID, testifymock.Anything).
		Return(nil)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.ErrorContains(t, err, "create session")
	require.ErrorContains(t, err, "user id is nil")
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenUsesTokenSessionIDForRevoke(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	newSessionID := uuid.New()
	storedSessionID := uuid.New()
	secret := "secret-value"
	accessToken := "new-access-token"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	storedUser := domain.User{
		ID:           userID,
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
	}

	sessions.EXPECT().
		GetByID(testifymock.Anything, sessionID).
		Return(domain.Session{
			ID:        storedSessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(storedUser, nil)
	sessions.EXPECT().
		Revoke(testifymock.Anything, sessionID, testifymock.Anything).
		Return(nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.Session{ID: newSessionID, UserID: userID}, nil)
	issuer.EXPECT().IssueToken(storedUser).Return(accessToken, nil)

	result, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(sessionID.String(), secret)})

	require.NoError(t, err)
	require.Equal(t, accessToken, result.AccessToken)
	require.True(t, strings.HasPrefix(result.RefreshToken, newSessionID.String()+"."))
}

func TestRefreshTokenRejectsInvalidSessionID(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken("not-a-uuid", "secret")})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsNilSessionID(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{
		RefreshToken: formatSessionToken(uuid.Nil.String(), "secret"),
	})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Revoke", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenRejectsAmbiguousFormat(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: uuid.NewString() + ".secret.extra"})

	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	repo.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	sessions.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestRefreshTokenAccessTokenErrorRollsBack(t *testing.T) {
	userID := uuid.New()
	oldSessionID := uuid.New()
	newSessionID := uuid.New()
	secret := "secret-value"

	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newRecordingProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	storedUser := domain.User{
		ID:           userID,
		Email:        registeredEmail,
		PasswordHash: "stored-hash",
		Role:         domain.UserRoleUser,
		Status:       domain.UserStatusActive,
	}

	sessions.EXPECT().
		GetByID(testifymock.Anything, oldSessionID).
		Return(domain.Session{
			ID:        oldSessionID,
			UserID:    userID,
			TokenHash: hashSessionSecret(secret),
			ExpiresAt: time.Now().UTC().Add(testSessionTTL),
		}, nil)
	repo.EXPECT().
		GetByID(testifymock.Anything, userID).
		Return(storedUser, nil)
	sessions.EXPECT().
		Revoke(testifymock.Anything, oldSessionID, testifymock.Anything).
		Return(nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		Return(domain.Session{ID: newSessionID, UserID: userID}, nil)
	issuer.EXPECT().IssueToken(storedUser).Return("", errors.New("jwt down"))

	_, err := auth.RefreshToken(context.Background(), RefreshTokenInput{RefreshToken: formatSessionToken(oldSessionID.String(), secret)})

	require.ErrorContains(t, err, "issue access token")
	require.False(t, txProvider.committed)
	require.True(t, txProvider.rolledBack)
}

func TestGetMyProfile(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(repo *outboundmocks.MockUserRepository, userID uuid.UUID)
		expected    GetProfileResult
		errIs       error
		errContains string
	}{
		{
			name: "success",
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					GetByID(testifymock.Anything, userID).
					Return(domain.User{
						ID:          userID,
						Email:       registeredEmail,
						DisplayName: "John Doe",
						Role:        domain.UserRoleAdmin,
						Status:      domain.UserStatusActive,
					}, nil)
			},
			expected: GetProfileResult{
				UserID:      "",
				Email:       registeredEmail,
				DisplayName: new("John Doe"),
				Role:        domain.UserRoleAdmin,
				Status:      domain.UserStatusActive,
			},
		},
		{
			name: "user not found",
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					GetByID(testifymock.Anything, userID).
					Return(domain.User{}, outbound.ErrUserNotFound)
			},
			errIs: outbound.ErrUserNotFound,
		},
		{
			name: "repo error",
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					GetByID(testifymock.Anything, userID).
					Return(domain.User{}, errors.New("db down"))
			},
			errContains: "get user by id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			tt.setup(repo, userID)

			result, err := auth.GetMyProfile(context.Background(), userID)

			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
				require.Equal(t, GetProfileResult{}, result)
				return
			}

			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				require.Equal(t, GetProfileResult{}, result)
				return
			}

			require.NoError(t, err)
			tt.expected.UserID = userID.String()
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateMyProfile(t *testing.T) {
	tests := []struct {
		name              string
		input             UpdateProfileInput
		setup             func(repo *outboundmocks.MockUserRepository, userID uuid.UUID)
		expected          UpdateProfileResult
		expectedErrIs     error
		expectedErrText   string
		assertUpdateCalls bool
	}{
		{
			name:  "success",
			input: UpdateProfileInput{DisplayName: new("  John Updated  ")},
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					Update(testifymock.Anything, userID, outbound.UserUpdateParams{DisplayName: "  John Updated  "}).
					Return(domain.User{
						ID:          userID,
						Email:       registeredEmail,
						DisplayName: "  John Updated  ",
						Role:        domain.UserRoleUser,
						Status:      domain.UserStatusActive,
					}, nil)
			},
			expected: UpdateProfileResult{
				UserID:      "",
				Email:       registeredEmail,
				DisplayName: new("  John Updated  "),
				Role:        domain.UserRoleUser,
				Status:      domain.UserStatusActive,
			},
			assertUpdateCalls: true,
		},
		{
			name:  "success nil displayName",
			input: UpdateProfileInput{},
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					GetByID(testifymock.Anything, userID).
					Return(domain.User{
						ID:          userID,
						Email:       registeredEmail,
						DisplayName: "Existing Name",
						Role:        domain.UserRoleUser,
						Status:      domain.UserStatusActive,
					}, nil)
			},
			expected: UpdateProfileResult{
				UserID:      "",
				Email:       registeredEmail,
				DisplayName: new("Existing Name"),
				Role:        domain.UserRoleUser,
				Status:      domain.UserStatusActive,
			},
			assertUpdateCalls: false,
		},
		{
			name:  "user not found",
			input: UpdateProfileInput{DisplayName: new("John Updated")},
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					Update(testifymock.Anything, userID, outbound.UserUpdateParams{DisplayName: "John Updated"}).
					Return(domain.User{}, outbound.ErrUserNotFound)
			},
			expectedErrIs:     outbound.ErrUserNotFound,
			assertUpdateCalls: true,
		},
		{
			name:  "repo error",
			input: UpdateProfileInput{DisplayName: new("John Updated")},
			setup: func(repo *outboundmocks.MockUserRepository, userID uuid.UUID) {
				repo.EXPECT().
					Update(testifymock.Anything, userID, outbound.UserUpdateParams{DisplayName: "John Updated"}).
					Return(domain.User{}, errors.New("db down"))
			},
			expectedErrText:   "update user by id",
			assertUpdateCalls: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			repo := outboundmocks.NewMockUserRepository(t)
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			txProvider := newStubProvider(repo, sessions)
			auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

			if tt.setup != nil {
				tt.setup(repo, userID)
			}

			result, err := auth.UpdateMyProfile(context.Background(), userID, tt.input)

			if tt.expectedErrIs != nil {
				require.ErrorIs(t, err, tt.expectedErrIs)
				require.Equal(t, UpdateProfileResult{}, result)
				if tt.expectedErrText != "" {
					require.ErrorContains(t, err, tt.expectedErrText)
				}
			} else if tt.expectedErrText != "" {
				require.ErrorContains(t, err, tt.expectedErrText)
				require.Equal(t, UpdateProfileResult{}, result)
			} else {
				require.NoError(t, err)
				tt.expected.UserID = userID.String()
				require.Equal(t, tt.expected, result)
			}

			if !tt.assertUpdateCalls {
				repo.AssertNotCalled(t, "Update", testifymock.Anything, testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

//go:fix inline
func stringPtr(value string) *string {
	return new(value)
}

// stubProvider implements tx.Provider[service.IdentityRepos] for tests.
type stubProvider struct {
	repos  IdentityRepos
	err    error
	called bool
}

type recordingProvider struct {
	repos      IdentityRepos
	committed  bool
	rolledBack bool
}

func newStubProvider(users outbound.UserRepository, sessions outbound.SessionRepository) *stubProvider {
	return &stubProvider{
		repos: IdentityRepos{Users: users, Sessions: sessions},
	}
}

func newRecordingProvider(users outbound.UserRepository, sessions outbound.SessionRepository) *recordingProvider {
	return &recordingProvider{
		repos: IdentityRepos{Users: users, Sessions: sessions},
	}
}

func (p *stubProvider) WithTransaction(_ context.Context, txOpts *sql.TxOptions, fn func(uow tx.UnitOfWork[IdentityRepos]) error) error {
	p.called = true
	if p.err != nil {
		return p.err
	}
	return fn(&stubUnitOfWork{repos: p.repos})
}

func (p *recordingProvider) WithTransaction(_ context.Context, txOpts *sql.TxOptions, fn func(uow tx.UnitOfWork[IdentityRepos]) error) error {
	err := fn(&stubUnitOfWork{repos: p.repos})
	if err != nil {
		p.rolledBack = true
		return err
	}

	p.committed = true
	return nil
}

type stubUnitOfWork struct {
	repos IdentityRepos
}

func (u stubUnitOfWork) Repos() IdentityRepos {
	return u.repos
}
