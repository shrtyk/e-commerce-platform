package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http/dto"
)

func NewRouter() http.Handler {
	handler := NewIdentityHandler()
	r := chi.NewRouter()
	r.Get("/healthz", handler.Healthz)

	return dto.HandlerFromMux(dto.Unimplemented{}, r)
}
