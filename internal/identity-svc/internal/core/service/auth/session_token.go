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
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/ports/outbound"
)

var (
	errNilUserID    = errors.New("user id is nil")
	errNilSessionID = errors.New("session id is nil")
)

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
