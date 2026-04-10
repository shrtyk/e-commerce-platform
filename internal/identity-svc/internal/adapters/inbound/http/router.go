package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
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
	dto.HandlerFromMux(&publicHandler{handler: handler}, r)

	profileRouter := r.With(provider.Auth())
	profileRouter.Get("/v1/profile/me", handler.GetMyProfile)
	profileRouter.Patch("/v1/profile/me", handler.UpdateMyProfile)

	return r
}

type publicHandler struct {
	dto.Unimplemented

	handler *IdentityHandler
}

func (h *publicHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	h.handler.RegisterUser(w, r)
}

func (h *publicHandler) LoginUser(w http.ResponseWriter, r *http.Request) {
	h.handler.LoginUser(w, r)
}

func (h *publicHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	h.handler.RefreshToken(w, r)
}
