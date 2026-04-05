package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandlerRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		statusCode int
		body       string
	}{
		{
			name:       "healthz",
			method:     http.MethodGet,
			target:     "/healthz",
			statusCode: http.StatusOK,
			body:       "ok",
		},
		{
			name:       "login route",
			method:     http.MethodPost,
			target:     "/v1/auth/login",
			statusCode: http.StatusNotImplemented,
			body:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewRouter()
			req := httptest.NewRequest(tt.method, tt.target, nil)
			res := httptest.NewRecorder()

			h.ServeHTTP(res, req)

			is := assert.New(t)
			is.Equal(tt.statusCode, res.Code)
			if tt.body != "" {
				is.Equal(tt.body, res.Body.String())
			}
		})
	}
}
