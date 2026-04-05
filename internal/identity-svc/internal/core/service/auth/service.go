package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

var (
	ErrEmailAlreadyRegistered = errors.New("identity email already registered")
	ErrInvalidRegisterInput   = errors.New("identity register input is invalid")
	ErrInvalidCredentials     = errors.New("identity invalid credentials")
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
	Status       domain.UserStatus
	RefreshToken string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type LoginUserInput struct {
	Email    string
	Password string
}

type LoginUserResult RegisterUserResult

type AuthService struct {
	users      outbound.UserRepository
	sessions   outbound.SessionRepository
	hasher     outbound.PasswordHasher
	sessionTTL time.Duration
}

func NewAuthService(
	users outbound.UserRepository,
	sessions outbound.SessionRepository,
	hasher outbound.PasswordHasher,
	sessionTTL time.Duration,
) *AuthService {
	return &AuthService{
		users:      users,
		sessions:   sessions,
		hasher:     hasher,
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

	existingUser, err := s.users.GetByEmail(ctx, email)
	if err == nil && existingUser != nil {
		return RegisterUserResult{}, ErrEmailAlreadyRegistered
	}
	if err != nil && !errors.Is(err, outbound.ErrUserNotFound) {
		return RegisterUserResult{}, fmt.Errorf("lookup user by email: %w", err)
	}

	passwordHash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return RegisterUserResult{}, fmt.Errorf("hash password: %w", err)
	}

	user := domain.User{
		Email:        email,
		PasswordHash: passwordHash,
		DisplayName:  normalizeDisplayName(input.DisplayName),
		Status:       domain.UserStatusActive,
	}

	createdUser, err := s.users.Create(ctx, user)
	if err != nil {
		return RegisterUserResult{}, fmt.Errorf("create user: %w", err)
	}

	refreshToken, err := s.createSession(ctx, createdUser.ID)
	if err != nil {
		return RegisterUserResult{}, err
	}

	result := toRegisterUserResult(createdUser)
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

	user, err := s.users.GetByEmail(ctx, email)
	if errors.Is(err, outbound.ErrUserNotFound) {
		return LoginUserResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginUserResult{}, fmt.Errorf("lookup user by email: %w", err)
	}

	verified, err := s.hasher.Verify(input.Password, user.PasswordHash)
	if err != nil {
		return LoginUserResult{}, fmt.Errorf("verify password: %w", err)
	}
	if !verified || user.Status != domain.UserStatusActive {
		return LoginUserResult{}, ErrInvalidCredentials
	}

	refreshToken, err := s.createSession(ctx, user.ID)
	if err != nil {
		return LoginUserResult{}, err
	}

	result := toLoginUserResult(*user)
	result.RefreshToken = refreshToken

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
