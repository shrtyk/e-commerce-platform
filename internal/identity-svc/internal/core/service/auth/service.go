package auth

import (
	"context"
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

type IdentityRepos struct {
	Users    outbound.UserRepository
	Sessions outbound.SessionRepository
}

type AuthService struct {
	repos      IdentityRepos
	txProvider tx.Provider[IdentityRepos]
	hasher     outbound.PasswordHasher
	tokens     outbound.TokenIssuer
	sessionTTL time.Duration
}

func NewAuthService(
	users outbound.UserRepository,
	sessions outbound.SessionRepository,
	txProvider tx.Provider[IdentityRepos],
	hasher outbound.PasswordHasher,
	tokens outbound.TokenIssuer,
	sessionTTL time.Duration,
) *AuthService {
	return &AuthService{
		repos: IdentityRepos{
			Users:    users,
			Sessions: sessions,
		},
		txProvider: txProvider,
		hasher:     hasher,
		tokens:     tokens,
		sessionTTL: sessionTTL,
	}
}

func (s *AuthService) RegisterUser(
	ctx context.Context,
	input RegisterUserInput,
) (RegisterUserResult, error) {
	email := normalizeEmail(input.Email)
	if email == "" || input.Password == "" {
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
		Role:         domain.UserRoleUser,
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

func (s *AuthService) LoginUser(
	ctx context.Context,
	input LoginUserInput,
) (LoginUserResult, error) {
	email := normalizeEmail(input.Email)
	if email == "" || input.Password == "" {
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
