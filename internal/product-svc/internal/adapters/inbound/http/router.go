package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http/dto"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	catalogService catalogService,
	tracer trace.Tracer,
) http.Handler {
	r := chi.NewRouter()

	provider := httpcommon.NewMiddlewaresProvider(serviceName, logger, tracer)
	r.Use(
		provider.RequestID,
		provider.Tracing,
		provider.RequestLogger,
		provider.Recovery,
	)

	handler := NewCatalogHandler(catalogService)
	r.Get("/healthz", handler.Healthz)
	dto.HandlerWithOptions(handler, dto.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: handler.HandleOpenAPIError,
	})

	return r
}
