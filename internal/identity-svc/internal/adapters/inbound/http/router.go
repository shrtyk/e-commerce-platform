package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
)

func NewRouter(logger *slog.Logger, serviceName string) http.Handler {
	r := chi.NewRouter()

	provider := httpcommon.NewMiddlewaresProvider(serviceName, logger)
	r.Use(
		provider.RequestID,
		provider.RequestLogger,
		provider.Recovery,
	)

	handler := NewIdentityHandler()
	r.Get("/healthz", handler.Healthz)

	return dto.HandlerFromMux(dto.Unimplemented{}, r)
}
