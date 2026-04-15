package http

import (
	"net/http"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http/dto"
	commonerrors "github.com/shrtyk/e-commerce-platform/internal/common/errors"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

type CartHandler struct {
	dto.Unimplemented
}

func NewCartHandler() *CartHandler {
	return &CartHandler{}
}

func (h *CartHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *CartHandler) HandleOpenAPIError(w http.ResponseWriter, r *http.Request, _ error) {
	h.writeError(w, r, commonerrors.BadRequest("invalid_request", "invalid request parameters"))
}

func (h *CartHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httpErr := commonerrors.FromError(err)
	commonerrors.WriteJSON(w, httpErr, transport.RequestIDFromContext(r.Context()))
}
