package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	authService *auth.AuthService,
	tokenVerifier httpcommon.TokenVerifier,
	tracer trace.Tracer,
) http.Handler {
	r := chi.NewRouter()

	provider := httpcommon.NewMiddlewaresProviderWithAuth(serviceName, logger, tokenVerifier, tracer)
	r.Use(
		provider.RequestID,
		provider.Tracing,
		provider.RequestLogger,
		provider.Recovery,
	)

	handler := NewIdentityHandler(authService)
	r.Get("/healthz", handler.Healthz)
	dto.HandlerFromMux(&publicHandler{
		handler:     handler,
		adminAuthMw: adminAuthMiddlewareJSON(tokenVerifier),
	}, r)

	profileRouter := r.With(provider.Auth())
	profileRouter.Get("/v1/profile/me", handler.GetMyProfile)
	profileRouter.Patch("/v1/profile/me", handler.UpdateMyProfile)

	return r
}

type publicHandler struct {
	dto.Unimplemented

	handler     *IdentityHandler
	adminAuthMw func(http.Handler) http.Handler
}

func (h *publicHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	h.handler.RegisterUser(w, r)
}

func (h *publicHandler) RegisterAdmin(w http.ResponseWriter, r *http.Request) {
	h.adminAuthMw(http.HandlerFunc(h.handler.RegisterAdmin)).ServeHTTP(w, r)
}

func (h *publicHandler) LoginUser(w http.ResponseWriter, r *http.Request) {
	h.handler.LoginUser(w, r)
}

func (h *publicHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	h.handler.RefreshToken(w, r)
}

func adminAuthMiddlewareJSON(tokenVerifier httpcommon.TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tokenVerifier == nil {
				writeUnauthorized(w, r)
				return
			}

			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			scheme, token, ok := strings.Cut(authHeader, " ")
			token = strings.TrimSpace(token)
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				writeUnauthorized(w, r)
				return
			}

			claims, err := tokenVerifier.Verify(token)
			if err != nil {
				writeUnauthorized(w, r)
				return
			}

			if claims.Role != string(domain.UserRoleAdmin) {
				commonerrors.WriteJSON(
					w,
					commonerrors.NewHTTPError("forbidden", "forbidden", http.StatusForbidden),
					transport.RequestIDFromContext(r.Context()),
				)
				return
			}

			next.ServeHTTP(w, r.WithContext(transport.WithClaims(r.Context(), claims)))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	commonerrors.WriteJSON(
		w,
		commonerrors.Unauthorized("unauthorized", "unauthorized"),
		transport.RequestIDFromContext(r.Context()),
	)
}
