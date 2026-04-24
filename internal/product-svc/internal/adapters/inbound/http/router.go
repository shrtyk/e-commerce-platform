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
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	catalogService catalogService,
	handlerConfig CatalogHandlerConfig,
	tracer trace.Tracer,
	tokenVerifier ...httpcommon.TokenVerifier,
) http.Handler {
	r := chi.NewRouter()

	var verifier httpcommon.TokenVerifier
	if len(tokenVerifier) > 0 {
		verifier = tokenVerifier[0]
	}

	provider := httpcommon.NewMiddlewaresProviderWithAuth(serviceName, logger, verifier, tracer)
	r.Use(
		provider.RequestID,
		provider.Tracing,
		provider.RequestLogger,
		provider.Recovery,
	)

	handler := NewCatalogHandler(catalogService, handlerConfig)
	r.Get("/healthz", handler.Healthz)
	dto.HandlerWithOptions(&publicHandler{
		handler:     handler,
		adminAuthMw: adminAuthMiddlewareJSON(verifier),
	}, dto.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: handler.HandleOpenAPIError,
	})

	return r
}

type publicHandler struct {
	dto.Unimplemented

	handler     *CatalogHandler
	adminAuthMw func(http.Handler) http.Handler
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

			if claims.Role != "admin" {
				commonerrors.WriteJSON(
					w,
					commonerrors.NewHTTPError("forbidden", "forbidden", http.StatusForbidden),
					transport.RequestIDFromContext(r.Context()),
				)
				return
			}

			if claims.Status != "active" {
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

func (h *publicHandler) ListPublishedProducts(w http.ResponseWriter, r *http.Request) {
	h.handler.ListPublishedProducts(w, r)
}

func (h *publicHandler) GetProductById(w http.ResponseWriter, r *http.Request, productId dto.ProductId) {
	h.handler.GetProductById(w, r, productId)
}

func (h *publicHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	h.adminAuthMw(http.HandlerFunc(h.handler.CreateProduct)).ServeHTTP(w, r)
}

func (h *publicHandler) UpdateProductById(w http.ResponseWriter, r *http.Request, productId dto.ProductId) {
	h.adminAuthMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handler.UpdateProductById(w, r, productId)
	})).ServeHTTP(w, r)
}

func (h *publicHandler) DeleteProductById(w http.ResponseWriter, r *http.Request, productId dto.ProductId) {
	h.adminAuthMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handler.DeleteProductById(w, r, productId)
	})).ServeHTTP(w, r)
}
