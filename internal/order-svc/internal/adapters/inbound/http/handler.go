package http

import (
	"context"
	"net/http"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http/dto"
)

const defaultReadyzTimeout = time.Second

type readinessChecker interface {
	PingContext(context.Context) error
}

type OrderHandler struct {
	readinessChecker readinessChecker
	readinessTimeout time.Duration
}

func NewOrderHandler(readinessChecker readinessChecker, readinessTimeout time.Duration) *OrderHandler {
	if readinessTimeout <= 0 {
		readinessTimeout = defaultReadyzTimeout
	}

	return &OrderHandler{
		readinessChecker: readinessChecker,
		readinessTimeout: readinessTimeout,
	}
}

func (h *OrderHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *OrderHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	if h.readinessChecker == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.readinessTimeout)
	defer cancel()

	if err := h.readinessChecker.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *OrderHandler) CreateOrder(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
}

func (h *OrderHandler) GetOrderById(w http.ResponseWriter, _ *http.Request, _ dto.OrderId) {
	w.WriteHeader(http.StatusNotImplemented)
}
