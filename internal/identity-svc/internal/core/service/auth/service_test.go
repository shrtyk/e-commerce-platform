package auth

import (
	"context"
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
	pwd := "pwd"
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
			ID:           userUUID.String(),
			Email:        normalizedEmail,
			PasswordHash: pwdHash,
			DisplayName:  normalizedName,
			Role:         domain.UserRoleUser,
			Status:       domain.UserStatusActive,
		}, nil)
	sessions.EXPECT().
		Create(testifymock.Anything, testifymock.Anything).
		RunAndReturn(func(_ context.Context, session domain.Session) (domain.Session, error) {
			require.Equal(t, userUUID.String(), session.UserID)
			require.NotEmpty(t, session.TokenHash)
			require.WithinDuration(t, time.Now().UTC().Add(testSessionTTL), session.ExpiresAt, time.Second)

			return domain.Session{
				ID:        sessionID.String(),
				UserID:    session.UserID,
				TokenHash: session.TokenHash,
				ExpiresAt: session.ExpiresAt,
			}, nil
		})
	issuer.EXPECT().IssueToken(testifymock.MatchedBy(func(user domain.User) bool {
		return user.ID == userUUID.String() && user.Email == normalizedEmail && user.Role == domain.UserRoleUser
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
		Return(domain.User{ID: userUUID.String(), Email: registeredEmail, Status: domain.UserStatusActive}, nil)
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

func TestRegisterUserAccessTokenError(t *testing.T) {
	userUUID := uuid.New()
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	txProvider := newStubProvider(repo, sessions)
	auth := NewAuthService(repo, sessions, txProvider, hasher, issuer, testSessionTTL)

	hasher.EXPECT().Hash(strongPassword).Return("hash", nil)
	repo.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.User{ID: userUUID.String(), Email: registeredEmail, Status: domain.UserStatusActive}, nil)
	sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: uuid.NewString(), UserID: userUUID.String()}, nil)
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

func TestLoginUserReturnsUser(t *testing.T) {
	accessToken := "access-token"
	userUUID := uuid.New()
	normalizedEmail := registeredEmail
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	auth := NewAuthService(repo, sessions, nil, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
		ID:           userUUID.String(),
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
				ID:        sessionID.String(),
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
	require.Equal(t, storedUser.ID, result.ID)
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
			sessions := outboundmocks.NewMockSessionRepository(t)
			hasher := outboundmocks.NewMockPasswordHasher(t)
			issuer := outboundmocks.NewMockTokenIssuer(t)
			auth := NewAuthService(repo, sessions, nil, hasher, issuer, testSessionTTL)

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

func TestLoginUserVerifyError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	auth := NewAuthService(repo, sessions, nil, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
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
	sessions.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	issuer.AssertNotCalled(t, "IssueToken", testifymock.Anything)
}

func TestLoginUserSessionError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	auth := NewAuthService(repo, sessions, nil, hasher, issuer, testSessionTTL)
	storedUser := domain.User{
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

func TestLoginUserAccessTokenError(t *testing.T) {
	repo := outboundmocks.NewMockUserRepository(t)
	sessions := outboundmocks.NewMockSessionRepository(t)
	hasher := outboundmocks.NewMockPasswordHasher(t)
	issuer := outboundmocks.NewMockTokenIssuer(t)
	auth := NewAuthService(repo, sessions, nil, hasher, issuer, testSessionTTL)
	storedUser := domain.User{ID: uuid.NewString(), Email: registeredEmail, PasswordHash: "stored-hash", Status: domain.UserStatusActive}

	repo.EXPECT().GetByEmail(testifymock.Anything, registeredEmail).Return(storedUser, nil)
	hasher.EXPECT().Verify(strongPassword, storedUser.PasswordHash).Return(true, nil)
	sessions.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.Session{ID: uuid.NewString(), UserID: storedUser.ID}, nil)
	issuer.EXPECT().IssueToken(testifymock.Anything).Return("", errors.New("jwt down"))

	_, err := auth.LoginUser(context.Background(), LoginUserInput{Email: registeredEmail, Password: strongPassword})

	require.ErrorContains(t, err, "issue access token")
}

// stubProvider implements tx.Provider[service.IdentityRepos] for tests.
type stubProvider struct {
	repos  IdentityRepos
	err    error
	called bool
}

func newStubProvider(users outbound.UserRepository, sessions outbound.SessionRepository) *stubProvider {
	return &stubProvider{
		repos: IdentityRepos{Users: users, Sessions: sessions},
	}
}

func (p *stubProvider) WithTransaction(_ context.Context, fn func(uow tx.UnitOfWork[IdentityRepos]) error) error {
	p.called = true
	if p.err != nil {
		return p.err
	}
	return fn(&stubUnitOfWork{repos: p.repos})
}

type stubUnitOfWork struct {
	repos IdentityRepos
}

func (u stubUnitOfWork) Repos() IdentityRepos {
	return u.repos
}
