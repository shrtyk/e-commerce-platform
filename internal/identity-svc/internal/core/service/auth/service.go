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
	policy     PasswordPolicy
}

type PasswordPolicy struct {
	MinLength int
}

const defaultMinPasswordLength = 8

func normalizePasswordPolicy(policy PasswordPolicy) PasswordPolicy {
	if policy.MinLength <= 0 {
		policy.MinLength = defaultMinPasswordLength
	}

	return policy
}

func NewAuthService(
	users outbound.UserRepository,
	sessions outbound.SessionRepository,
	txProvider tx.Provider[IdentityRepos],
	hasher outbound.PasswordHasher,
	tokens outbound.TokenIssuer,
	sessionTTL time.Duration,
	policies ...PasswordPolicy,
) *AuthService {
	policy := PasswordPolicy{}
	if len(policies) > 0 {
		policy = policies[0]
	}

	policy = normalizePasswordPolicy(policy)

	return &AuthService{
		repos: IdentityRepos{
			Users:    users,
			Sessions: sessions,
		},
		txProvider: txProvider,
		hasher:     hasher,
		tokens:     tokens,
		sessionTTL: sessionTTL,
		policy:     policy,
	}
}

func (s *AuthService) minPasswordLength() int {
	if s == nil {
		return defaultMinPasswordLength
	}

	min := s.policy.MinLength
	if min <= 0 {
		return defaultMinPasswordLength
	}

	return min
}
