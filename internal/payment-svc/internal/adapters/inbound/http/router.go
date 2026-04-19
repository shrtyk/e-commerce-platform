package http

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
)

func NewRouter(
	logger *slog.Logger,
	serviceName string,
	db *sql.DB,
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

	var checker readinessChecker
	if db != nil {
		checker = db
	}

	handler := NewHandler(checker, 0)
	r.Get("/healthz", handler.Healthz)
	r.Get("/readyz", handler.Readyz)

	return r
}
