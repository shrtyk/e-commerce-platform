package http

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http/dto"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	db *sql.DB,
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

	handler := NewOrderHandler(db, 0)
	r.Get("/healthz", handler.Healthz)
	r.Get("/readyz", handler.Readyz)

	openAPIOptions := dto.ChiServerOptions{BaseRouter: r}
	openAPIOptions.Middlewares = []dto.MiddlewareFunc{provider.Auth()}

	dto.HandlerWithOptions(handler, openAPIOptions)

	return r
}
