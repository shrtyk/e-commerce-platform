package http

import (
	"context"
	"net/http"
	"time"
)

const defaultReadyzTimeout = time.Second

type readinessChecker interface {
	PingContext(context.Context) error
}

type Handler struct {
	readinessChecker readinessChecker
	readinessTimeout time.Duration
}

func NewHandler(readinessChecker readinessChecker, readinessTimeout time.Duration) *Handler {
	if readinessTimeout <= 0 {
		readinessTimeout = defaultReadyzTimeout
	}

	return &Handler{
		readinessChecker: readinessChecker,
		readinessTimeout: readinessTimeout,
	}
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
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
