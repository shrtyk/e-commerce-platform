package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

var (
	ErrEmailAlreadyRegistered = errors.New("identity email already registered")
	ErrInvalidRegisterInput   = errors.New("identity register input is invalid")
	ErrInvalidCredentials     = errors.New("identity invalid credentials")
	ErrInvalidRefreshToken    = errors.New("identity invalid refresh token")
)

const (
	maxPasswordLength = 72
)

var (
	errNilUserID    = errors.New("user id is nil")
	errNilSessionID = errors.New("session id is nil")
)

type RegisterUserInput struct {
	Email       string
	Password    string
	DisplayName *string
}

type RegisterUserResult struct {
	ID           string
	Email        string
	DisplayName  string
	Role         domain.UserRole
	Status       domain.UserStatus
	AccessToken  string
	RefreshToken string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type LoginUserInput struct {
	Email    string
	Password string
}

type LoginUserResult RegisterUserResult

type RefreshTokenInput struct {
	RefreshToken string
}

type RefreshTokenResult LoginUserResult

type BootstrapAdminInput struct {
	Email       string
	Password    string
	DisplayName *string
}

func (s *AuthService) RegisterUser(
	ctx context.Context,
	input RegisterUserInput,
) (RegisterUserResult, error) {
	return s.registerUserWithRole(ctx, input, domain.UserRoleUser)
}

func (s *AuthService) RegisterAdmin(
	ctx context.Context,
	input RegisterUserInput,
) (RegisterUserResult, error) {
	return s.registerUserWithRole(ctx, input, domain.UserRoleAdmin)
}

func (s *AuthService) registerUserWithRole(
	ctx context.Context,
	input RegisterUserInput,
	role domain.UserRole,
) (RegisterUserResult, error) {
	email := normalizeEmail(input.Email)
	if email == "" || !isPasswordLengthValid(input.Password, s.minPasswordLength()) {
		return RegisterUserResult{}, ErrInvalidRegisterInput
	}

	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return RegisterUserResult{}, fmt.Errorf("hash password: %w", err)
	}

	user := domain.User{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  normalizeDisplayName(input.DisplayName),
		Role:         role,
		Status:       domain.UserStatusActive,
	}

	var (
		createdUser  domain.User
		refreshToken string
	)

	err = s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[IdentityRepos]) error {
		repos := uow.Repos()
		var err error
		createdUser, err = repos.Users.Create(ctx, user)
		if err != nil {
			if errors.Is(err, outbound.ErrDuplicateEmail) {
				return ErrEmailAlreadyRegistered
			}
			return fmt.Errorf("create user: %w", err)
		}

		refreshToken, err = s.createSessionWithRepository(ctx, repos.Sessions, createdUser.ID)
		return err
	})
	if err != nil {
		return RegisterUserResult{}, err
	}

	result := toRegisterUserResult(createdUser)
	accessToken, err := s.tokens.IssueToken(createdUser)
	if err != nil {
		return RegisterUserResult{}, fmt.Errorf("issue access token: %w", err)
	}
	result.AccessToken = accessToken
	result.RefreshToken = refreshToken

	return result, nil
}

func (s *AuthService) EnsureBootstrapAdmin(ctx context.Context, input BootstrapAdminInput) error {
	email := normalizeEmail(input.Email)
	if email == "" || !isPasswordLengthValid(input.Password, s.minPasswordLength()) {
		return fmt.Errorf("bootstrap admin: %w", ErrInvalidRegisterInput)
	}

	existingUser, err := s.repos.Users.GetByEmail(ctx, email)
	if err == nil {
		if existingUser.Role == domain.UserRoleAdmin && existingUser.Status == domain.UserStatusActive {
			return nil
		}

		return fmt.Errorf(
			"bootstrap admin %q exists with incompatible role/status (role=%q status=%q)",
			email,
			existingUser.Role,
			existingUser.Status,
		)
	}

	if !errors.Is(err, outbound.ErrUserNotFound) {
		return fmt.Errorf("lookup bootstrap admin: %w", err)
	}

	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return fmt.Errorf("hash bootstrap admin password: %w", err)
	}

	_, err = s.repos.Users.Create(ctx, domain.User{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  normalizeDisplayName(input.DisplayName),
		Role:         domain.UserRoleAdmin,
		Status:       domain.UserStatusActive,
	})
	if err == nil {
		return nil
	}

	if !errors.Is(err, outbound.ErrDuplicateEmail) {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}

	existingUser, err = s.repos.Users.GetByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("lookup bootstrap admin after duplicate email: %w", err)
	}

	if existingUser.Role == domain.UserRoleAdmin && existingUser.Status == domain.UserStatusActive {
		return nil
	}

	return fmt.Errorf(
		"bootstrap admin %q exists with incompatible role/status (role=%q status=%q)",
		email,
		existingUser.Role,
		existingUser.Status,
	)
}

func (s *AuthService) LoginUser(
	ctx context.Context,
	input LoginUserInput,
) (LoginUserResult, error) {
	email := normalizeEmail(input.Email)
	if email == "" || !isPasswordLengthValid(input.Password, s.minPasswordLength()) {
		return LoginUserResult{}, ErrInvalidCredentials
	}

	var (
		user         domain.User
		accessToken  string
		refreshToken string
	)

	err := s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[IdentityRepos]) error {
		repos := uow.Repos()
		var err error

		user, err = repos.Users.GetByEmail(ctx, email)
		if errors.Is(err, outbound.ErrUserNotFound) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return fmt.Errorf("lookup user by email: %w", err)
		}

		verified, err := s.hasher.Verify(input.Password, user.PasswordHash)
		if err != nil {
			return fmt.Errorf("verify password: %w", err)
		}
		if !verified || user.Status != domain.UserStatusActive {
			return ErrInvalidCredentials
		}

		refreshToken, err = s.createSessionWithRepository(ctx, repos.Sessions, user.ID)
		if err != nil {
			return err
		}

		accessToken, err = s.tokens.IssueToken(user)
		if err != nil {
			return fmt.Errorf("issue access token: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginUserResult{}, err
	}

	result := toLoginUserResult(user)
	result.AccessToken = accessToken
	result.RefreshToken = refreshToken

	return result, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, input RefreshTokenInput) (RefreshTokenResult, error) {
	sessionID, secret, err := parseSessionToken(input.RefreshToken)
	if err != nil {
		return RefreshTokenResult{}, ErrInvalidRefreshToken
	}

	var (
		accessToken     string
		user            domain.User
		newRefreshToken string
	)

	err = s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[IdentityRepos]) error {
		repos := uow.Repos()

		session, err := repos.Sessions.GetByID(ctx, sessionID)
		if errors.Is(err, outbound.ErrSessionNotFound) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("get session by id: %w", err)
		}

		now := time.Now().UTC()
		if session.RevokedAt != nil || !session.ExpiresAt.After(now) || session.TokenHash != hashSessionSecret(secret) {
			return ErrInvalidRefreshToken
		}

		if session.UserID == uuid.Nil {
			return ErrInvalidRefreshToken
		}

		user, err = repos.Users.GetByID(ctx, session.UserID)
		if errors.Is(err, outbound.ErrUserNotFound) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("get user by id: %w", err)
		}
		if user.Status != domain.UserStatusActive {
			return ErrInvalidRefreshToken
		}

		if err := repos.Sessions.Revoke(ctx, sessionID, now); err != nil {
			if errors.Is(err, outbound.ErrSessionNotFound) {
				return ErrInvalidRefreshToken
			}

			return fmt.Errorf("revoke session: %w", err)
		}

		newRefreshToken, err = s.createSessionWithRepository(ctx, repos.Sessions, user.ID)
		if err != nil {
			return err
		}

		accessToken, err = s.tokens.IssueToken(user)
		if err != nil {
			return fmt.Errorf("issue access token: %w", err)
		}

		return nil
	})
	if err != nil {
		return RefreshTokenResult{}, err
	}

	result := RefreshTokenResult(toLoginUserResult(user))
	result.AccessToken = accessToken
	result.RefreshToken = newRefreshToken

	return result, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeDisplayName(displayName *string) string {
	if displayName == nil {
		return ""
	}

	return strings.TrimSpace(*displayName)
}

func isPasswordLengthValid(password string, minPasswordLength int) bool {
	passwordLength := len(password)

	return passwordLength >= minPasswordLength && passwordLength <= maxPasswordLength
}

// createSession creates a session using the default (non-tx) repository.
func (s *AuthService) createSession(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.createSessionWithRepository(ctx, s.repos.Sessions, userID)
}

// createSessionWithRepository creates a session using the provided repository.
// Accepts the repo which will be used in tx provider callback.
func (s *AuthService) createSessionWithRepository(
	ctx context.Context,
	sessions outbound.SessionRepository,
	userID uuid.UUID,
) (string, error) {
	if userID == uuid.Nil {
		return "", fmt.Errorf("create session: %w", errNilUserID)
	}

	secret, err := generateSessionSecret()
	if err != nil {
		return "", fmt.Errorf("generate session secret: %w", err)
	}

	createdSession, err := sessions.Create(ctx, domain.Session{
		UserID:    userID,
		TokenHash: hashSessionSecret(secret),
		ExpiresAt: time.Now().UTC().Add(s.sessionTTL),
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	if createdSession.ID == uuid.Nil {
		return "", fmt.Errorf("create session: %w", errNilSessionID)
	}

	return formatSessionToken(createdSession.ID.String(), secret), nil
}

func generateSessionSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashSessionSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func formatSessionToken(sessionID, secret string) string {
	return fmt.Sprintf("%s.%s", sessionID, secret)
}

func parseSessionToken(token string) (sessionID uuid.UUID, secret string, err error) {
	trimmedToken := strings.TrimSpace(token)
	if strings.Count(trimmedToken, ".") != 1 {
		return uuid.Nil, "", errors.New("invalid session token format")
	}

	sessionIDRaw, secret, found := strings.Cut(trimmedToken, ".")
	if !found || sessionIDRaw == "" || secret == "" {
		return uuid.Nil, "", errors.New("invalid session token format")
	}

	sessionID, err = uuid.Parse(sessionIDRaw)
	if err != nil {
		return uuid.Nil, "", errors.New("invalid session token format")
	}
	if sessionID == uuid.Nil {
		return uuid.Nil, "", errors.New("invalid session token format")
	}

	return sessionID, secret, nil
}

func toRegisterUserResult(user domain.User) RegisterUserResult {
	return RegisterUserResult{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func toLoginUserResult(user domain.User) LoginUserResult {
	return LoginUserResult(toRegisterUserResult(user))
}
