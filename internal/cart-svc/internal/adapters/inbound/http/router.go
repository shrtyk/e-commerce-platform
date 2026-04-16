package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http/dto"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	cartService cartService,
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

	handler := NewCartHandler(cartService)
	r.Get("/healthz", handler.Healthz)

	openAPIOptions := dto.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: handler.HandleOpenAPIError,
	}
	if tokenVerifier != nil {
		openAPIOptions.Middlewares = []dto.MiddlewareFunc{provider.Auth()}
	}

	dto.HandlerWithOptions(handler, openAPIOptions)

	return r
}
