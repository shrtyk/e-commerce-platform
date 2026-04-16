package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrderHandlerReadyz(t *testing.T) {
	tests := []struct {
		name       string
		checker    readinessChecker
		statusWant int
	}{
		{
			name:       "missing readiness checker",
			checker:    nil,
			statusWant: http.StatusServiceUnavailable,
		},
		{
			name:       "readiness check error",
			checker:    readinessCheckerStub{err: errors.New("db unavailable")},
			statusWant: http.StatusServiceUnavailable,
		},
		{
			name:       "ready",
			checker:    readinessCheckerStub{},
			statusWant: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewOrderHandler(tt.checker, 0)

			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			rr := httptest.NewRecorder()

			handler.Readyz(rr, req)

			require.Equal(t, tt.statusWant, rr.Code)
		})
	}
}

type readinessCheckerStub struct {
	err error
}

func (s readinessCheckerStub) PingContext(_ context.Context) error {
	return s.err
}
