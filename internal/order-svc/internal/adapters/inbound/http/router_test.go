package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewRouterProtectedByDefault(t *testing.T) {
	router := NewRouter(
		slog.Default(),
		"order-svc",
		nil,
		nil,
		noop.NewTracerProvider().Tracer("test"),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/orders", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestNewRouterHealthzStillPublic(t *testing.T) {
	router := NewRouter(
		slog.Default(),
		"order-svc",
		nil,
		nil,
		noop.NewTracerProvider().Tracer("test"),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
}
