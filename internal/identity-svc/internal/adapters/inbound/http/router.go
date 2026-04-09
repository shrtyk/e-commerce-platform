package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	authService *auth.AuthService,
	tokenVerifier httpcommon.TokenVerifier,
) http.Handler {
	r := chi.NewRouter()

	provider := httpcommon.NewMiddlewaresProviderWithAuth(serviceName, logger, tokenVerifier)
	r.Use(
		provider.RequestID,
		provider.RequestLogger,
		provider.Recovery,
	)

	handler := NewIdentityHandler(authService)
	r.Get("/healthz", handler.Healthz)

	// TODO: Apply auth middleware to profile routes when profile handlers are implemented.
	// dto.HandlerFromMux registers handlers via chi, so r.Use() applies globally;
	// profile-specific protection should be added as per-route middleware.

	return dto.HandlerFromMux(handler, r)
}
