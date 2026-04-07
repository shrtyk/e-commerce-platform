package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

// createSession creates a session using the default (non-tx) repository.
func (s *AuthService) createSession(ctx context.Context, userID string) (string, error) {
	return s.createSessionWithRepository(ctx, s.repos.Sessions, userID)
}

// createSessionWithRepository creates a session using the provided repository.
// Accepts the repo which will be used in tx provider callback.
func (s *AuthService) createSessionWithRepository(
	ctx context.Context,
	sessions outbound.SessionRepository,
	userID string,
) (string, error) {
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

	return formatSessionToken(createdSession.ID, secret), nil
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
