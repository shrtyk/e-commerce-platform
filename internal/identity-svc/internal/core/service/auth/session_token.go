package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
)

func (s *AuthService) createSession(ctx context.Context, userID string) (string, error) {
	secret, err := generateSessionSecret()
	if err != nil {
		return "", fmt.Errorf("generate session secret: %w", err)
	}

	createdSession, err := s.sessions.Create(ctx, domain.Session{
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
	return strings.Join([]string{sessionID, secret}, ".")
}
