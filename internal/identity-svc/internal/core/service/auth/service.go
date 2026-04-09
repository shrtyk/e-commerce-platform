package auth

import (
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

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
