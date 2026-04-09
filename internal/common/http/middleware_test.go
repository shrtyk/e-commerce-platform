package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRequestID(t *testing.T) {
	tests := []struct {
		name       string
		setupReq   func(*http.Request)
		checkResp  func(t *testing.T, rec *httptest.ResponseRecorder)
		checkCtx   func(t *testing.T, capturedID string)
		captureCtx bool
	}{
		{
			name:     "new id",
			setupReq: func(r *http.Request) {},
			checkResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.NotEmpty(t, rec.Header().Get("X-Request-ID"))
			},
		},
		{
			name: "preserves existing",
			setupReq: func(r *http.Request) {
				r.Header.Set("X-Request-ID", "existing-id-123")
			},
			checkResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "existing-id-123", rec.Header().Get("X-Request-ID"))
			},
		},
		{
			name:       "stores in context",
			setupReq:   func(r *http.Request) {},
			captureCtx: true,
			checkResp:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
			checkCtx: func(t *testing.T, capturedID string) {
				require.NotEmpty(t, capturedID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMiddlewaresProvider("test-service", slog.Default())
			var capturedID string
			handler := provider.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.captureCtx {
					capturedID = RequestIDFromContext(r.Context())
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setupReq(req)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			tt.checkResp(t, rec)
			if tt.checkCtx != nil {
				tt.checkCtx(t, capturedID)
			}
		})
	}
}

func TestRequestIDFromContextReturnsEmptyForMissingID(t *testing.T) {
	require.Empty(t, RequestIDFromContext(context.Background()))
}

func TestRequestLogger(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		check   func(t *testing.T, entry map[string]interface{})
	}{
		{
			name: "logs request with all fields",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			}),
			check: func(t *testing.T, entry map[string]interface{}) {
				require.Equal(t, "request", entry["msg"])
				require.Equal(t, "test-service", entry["service"])
				require.Equal(t, "POST", entry["method"])
				require.Equal(t, "/users", entry["path"])
				require.Equal(t, float64(201), entry["status"])
				require.NotNil(t, entry["duration_ms"])
			},
		},
		{
			name: "includes request id when chained",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			check: func(t *testing.T, entry map[string]interface{}) {
				requestID, ok := entry["request_id"].(string)
				require.True(t, ok)
				require.NotEmpty(t, requestID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			provider := NewMiddlewaresProvider("test-service", logger)

			var handler http.Handler
			if tt.name == "includes request id when chained" {
				handler = provider.RequestID(tt.handler)
				handler = provider.RequestLogger(handler)
			} else {
				handler = provider.RequestLogger(tt.handler)
			}

			method := http.MethodGet
			path := "/test"
			if tt.name == "logs request with all fields" {
				method = http.MethodPost
				path = "/users"
			}
			req := httptest.NewRequest(method, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			var entry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &entry)
			require.NoError(t, err)
			tt.check(t, entry)
		})
	}
}

func TestRecovery(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		check   func(t *testing.T, rec *httptest.ResponseRecorder, entry map[string]interface{})
	}{
		{
			name: "catches panic and returns 500",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("something went wrong")
			}),
			check: func(t *testing.T, rec *httptest.ResponseRecorder, entry map[string]interface{}) {
				require.Equal(t, http.StatusInternalServerError, rec.Code)
				require.True(t, strings.Contains(rec.Body.String(), "internal server error"))
			},
		},
		{
			name: "logs panic with stack",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("test panic")
			}),
			check: func(t *testing.T, rec *httptest.ResponseRecorder, entry map[string]interface{}) {
				require.Equal(t, "panic recovered", entry["msg"])
				require.Equal(t, "test panic", entry["panic"])
				stack, ok := entry["stack"].(string)
				require.True(t, ok)
				require.NotEmpty(t, stack)
			},
		},
		{
			name: "includes request id when chained",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("recovery test")
			}),
			check: func(t *testing.T, rec *httptest.ResponseRecorder, entry map[string]interface{}) {
				requestID := rec.Header().Get("X-Request-ID")
				logRequestID, ok := entry["request_id"].(string)
				require.True(t, ok)
				require.Equal(t, requestID, logRequestID)
				require.Equal(t, http.StatusInternalServerError, rec.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			provider := NewMiddlewaresProvider("test-service", logger)

			var handler http.Handler
			if tt.name == "includes request id when chained" {
				handler = provider.RequestID(tt.handler)
				handler = provider.Recovery(handler)
			} else {
				handler = provider.Recovery(tt.handler)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			require.NotPanics(t, func() {
				handler.ServeHTTP(rec, req)
			})

			var entry map[string]interface{}
			if tt.name != "catches panic and returns 500" {
				err := json.Unmarshal(buf.Bytes(), &entry)
				require.NoError(t, err)
			}
			tt.check(t, rec, entry)
		})
	}
}

func TestMiddlewareChain(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	provider := NewMiddlewaresProvider("test-service", logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux := provider.RequestID(handler)
	mux = provider.RequestLogger(mux)
	mux = provider.Recovery(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var entry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	requestID := rec.Header().Get("X-Request-ID")
	logRequestID, ok := entry["request_id"].(string)
	require.True(t, ok)
	require.Equal(t, requestID, logRequestID)
}

type tokenVerifierStub struct {
	claims Claims
	err    error
}

func (s tokenVerifierStub) Verify(_ string) (Claims, error) {
	return s.claims, s.err
}

func TestAuthMiddleware(t *testing.T) {
	validClaims := Claims{
		UserID: uuid.New(),
		Role:   "user",
		Status: "active",
	}

	tests := []struct {
		name          string
		provider      *MiddlewaresProvider
		requiredRoles []string
		authHeader    string
		wantStatus    int
		wantNext      bool
		assertContext func(t *testing.T, ctx context.Context)
	}{
		{
			name:          "valid token with matching role",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims, claims)
			},
		},
		{
			name:          "valid token with case insensitive bearer scheme",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims, claims)
			},
		},
		{
			name:          "missing authorization header",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}),
			requiredRoles: []string{"user"},
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "malformed authorization header",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}),
			requiredRoles: []string{"user"},
			authHeader:    "Bearer",
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "invalid token",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{err: errors.New("invalid token")}),
			requiredRoles: []string{"user"},
			authHeader:    "Bearer invalid-token",
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "wrong role",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: Claims{UserID: uuid.New(), Role: "guest", Status: "active"}}),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusForbidden,
			wantNext:      false,
		},
		{
			name:          "no role required",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}),
			requiredRoles: nil,
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims, claims)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			nextCalled := false

			handler := tt.provider.Auth(tt.requiredRoles...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantNext, nextCalled)
			if tt.assertContext != nil {
				tt.assertContext(t, capturedCtx)
			}
		})
	}
}

func TestWithClaims(t *testing.T) {
	claims := Claims{UserID: uuid.New(), Role: "admin", Status: "active"}

	ctx := WithClaims(context.Background(), claims)
	actual, ok := ClaimsFromContext(ctx)

	require.True(t, ok)
	require.Equal(t, claims, actual)
}

func TestClaimsFromContextWithoutClaims(t *testing.T) {
	claims, ok := ClaimsFromContext(context.Background())

	require.False(t, ok)
	require.Equal(t, Claims{}, claims)
}
