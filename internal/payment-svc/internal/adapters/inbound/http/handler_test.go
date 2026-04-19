package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandlerHealthz(t *testing.T) {
	handler := NewHandler(nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.Healthz(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}

func TestHandlerReadyz(t *testing.T) {
	t.Run("returns unavailable when checker missing", func(t *testing.T) {
		handler := NewHandler(nil, 0)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		handler.Readyz(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("returns unavailable when ping fails", func(t *testing.T) {
		handler := NewHandler(stubReadinessChecker{
			pingContextFunc: func(context.Context) error { return errors.New("db down") },
		}, 0)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		handler.Readyz(rec, req)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("returns ok when ping succeeds", func(t *testing.T) {
		handler := NewHandler(stubReadinessChecker{
			pingContextFunc: func(context.Context) error { return nil },
		}, 5*time.Millisecond)
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()

		handler.Readyz(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})
}

type stubReadinessChecker struct {
	pingContextFunc func(context.Context) error
}

func (s stubReadinessChecker) PingContext(ctx context.Context) error {
	if s.pingContextFunc == nil {
		return nil
	}

	return s.pingContextFunc(ctx)
}
