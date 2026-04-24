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
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
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
			provider := NewMiddlewaresProvider("test-service", slog.Default(), noop.NewTracerProvider().Tracer("test"))
			var capturedID string
			handler := provider.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.captureCtx {
					capturedID = transport.RequestIDFromContext(r.Context())
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
	require.Empty(t, transport.RequestIDFromContext(context.Background()))
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
				require.Equal(t, "", entry["trace_id"])
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
				require.Equal(t, "", entry["trace_id"])
			},
		},
		{
			name: "includes trace id from span context",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			check: func(t *testing.T, entry map[string]interface{}) {
				require.Equal(t, "01010101010101010101010101010101", entry["trace_id"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			provider := NewMiddlewaresProvider("test-service", logger, noop.NewTracerProvider().Tracer("test"))

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
			if tt.name == "includes trace id from span context" {
				sc := trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
					SpanID:     trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
					TraceFlags: trace.FlagsSampled,
				})
				req = req.WithContext(trace.ContextWithSpanContext(req.Context(), sc))
			}
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
				require.Equal(t, "test-service", entry["service"])
				require.Equal(t, "", entry["trace_id"])
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
				require.Equal(t, "", entry["trace_id"])
				require.Equal(t, http.StatusInternalServerError, rec.Code)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			provider := NewMiddlewaresProvider("test-service", logger, noop.NewTracerProvider().Tracer("test"))

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
	provider := NewMiddlewaresProvider("test-service", logger, noop.NewTracerProvider().Tracer("test"))

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
	claims transport.Claims
	err    error
}

func (s tokenVerifierStub) Verify(_ string) (transport.Claims, error) {
	return s.claims, s.err
}

func TestAuthMiddleware(t *testing.T) {
	validClaims := transport.Claims{
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
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := transport.ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims, claims)
			},
		},
		{
			name:          "valid token with case insensitive bearer scheme",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := transport.ClaimsFromContext(ctx)
				require.True(t, ok)
				require.Equal(t, validClaims, claims)
			},
		},
		{
			name:          "missing authorization header",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user"},
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "malformed authorization header",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user"},
			authHeader:    "Bearer",
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "invalid token",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{err: errors.New("invalid token")}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user"},
			authHeader:    "Bearer invalid-token",
			wantStatus:    http.StatusUnauthorized,
			wantNext:      false,
		},
		{
			name:          "wrong role",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: transport.Claims{UserID: uuid.New(), Role: "guest", Status: "active"}}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: []string{"user", "admin"},
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusForbidden,
			wantNext:      false,
		},
		{
			name:          "no role required",
			provider:      NewMiddlewaresProviderWithAuth("test-service", slog.Default(), tokenVerifierStub{claims: validClaims}, noop.NewTracerProvider().Tracer("test")),
			requiredRoles: nil,
			authHeader:    "Bearer valid-token",
			wantStatus:    http.StatusOK,
			wantNext:      true,
			assertContext: func(t *testing.T, ctx context.Context) {
				claims, ok := transport.ClaimsFromContext(ctx)
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
	claims := transport.Claims{UserID: uuid.New(), Role: "admin", Status: "active"}

	ctx := transport.WithClaims(context.Background(), claims)
	actual, ok := transport.ClaimsFromContext(ctx)

	require.True(t, ok)
	require.Equal(t, claims, actual)
}

func TestClaimsFromContextWithoutClaims(t *testing.T) {
	claims, ok := transport.ClaimsFromContext(context.Background())

	require.False(t, ok)
	require.Equal(t, transport.Claims{}, claims)
}

func TestTracing(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "creates span and sets trace header",
			statusCode: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spanRecorder := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
			tracer := tracerProvider.Tracer("test-http-tracer")

			provider := NewMiddlewaresProviderWithTracer("test-service", slog.Default(), tracer)
			handler := provider.Tracing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				span := trace.SpanFromContext(r.Context())
				require.True(t, span.SpanContext().IsValid())
				w.WriteHeader(tt.statusCode)
			}))

			req := httptest.NewRequest(http.MethodGet, "/trace", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, tt.statusCode, rec.Code)

			traceIDHeader := rec.Header().Get("X-Trace-ID")
			require.NotEmpty(t, traceIDHeader)

			spans := spanRecorder.Ended()
			require.Len(t, spans, 1)

			span := spans[0]
			require.Equal(t, "GET /trace", span.Name())
			require.Equal(t, traceIDHeader, span.SpanContext().TraceID().String())
			require.Contains(t, spanAttributes(span), attribute.Int("http.status_code", tt.statusCode))
		})
	}
}

func TestTracingExtractsRemoteParentContextFromHeaders(t *testing.T) {
	setW3CPropagator(t)

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test-http-tracer")

	provider := NewMiddlewaresProviderWithTracer("test-service", slog.Default(), tracer)
	handler := provider.Tracing(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	parentSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		SpanID:     trace.SpanID{2, 2, 2, 2, 2, 2, 2, 2},
		TraceFlags: trace.FlagsSampled,
	})

	requestHeaders := observability.InjectHTTPHeaders(trace.ContextWithSpanContext(context.Background(), parentSpanContext), nil)
	req := httptest.NewRequest(http.MethodGet, "/trace", nil)
	req.Header = requestHeaders
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	require.Equal(t, parentSpanContext.TraceID(), span.Parent().TraceID())
	require.Equal(t, parentSpanContext.SpanID(), span.Parent().SpanID())
}

func TestRequestLoggerEmitsNormalizedMetrics(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		statusCode     int
		expectedOperation string
		expectedStatus string
		expectedOutcome string
	}{
		{
			name:            "success response",
			path:            "/v1/orders/123/items",
			statusCode:      http.StatusCreated,
			expectedOperation: "/v1/orders/{id}/items",
			expectedStatus:  "2xx",
			expectedOutcome: "success",
		},
		{
			name:            "error response",
			path:            "/v1/orders/550e8400-e29b-41d4-a716-446655440000/items",
			statusCode:      http.StatusInternalServerError,
			expectedOperation: "/v1/orders/{id}/items",
			expectedStatus:  "5xx",
			expectedOutcome: "error",
		},
		{
			name:            "client error response",
			path:            "/v1/orders/abc123def456/items",
			statusCode:      http.StatusBadRequest,
			expectedOperation: "/v1/orders/{id}/items",
			expectedStatus:  "4xx",
			expectedOutcome: "error",
		},
		{
			name:              "preserves route names",
			path:              "/products/search",
			statusCode:        http.StatusOK,
			expectedOperation: "/products/search",
			expectedStatus:    "2xx",
			expectedOutcome:   "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

			previous := otel.GetMeterProvider()
			otel.SetMeterProvider(meterProvider)
			t.Cleanup(func() {
				require.NoError(t, meterProvider.Shutdown(context.Background()))
				otel.SetMeterProvider(previous)
			})

			provider := NewMiddlewaresProvider("test-service", slog.Default(), noop.NewTracerProvider().Tracer("test"))
			handler := provider.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			var rm metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &rm))

			requestTotal := findMetric(t, rm, observability.MetricNameRequestTotal)
			requestTotalData, ok := requestTotal.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, requestTotalData.DataPoints, 1)
			require.Equal(t, int64(1), requestTotalData.DataPoints[0].Value)
			assertAttributes(t, requestTotalData.DataPoints[0].Attributes, map[string]string{
				observability.MetricAttrTransport: "http",
				observability.MetricAttrOperation: tt.expectedOperation,
				observability.MetricAttrStatus:    tt.expectedStatus,
				observability.MetricAttrOutcome:   tt.expectedOutcome,
			})

			requestDuration := findMetric(t, rm, observability.MetricNameRequestDurationSeconds)
			requestDurationData, ok := requestDuration.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			require.Len(t, requestDurationData.DataPoints, 1)
			require.Equal(t, uint64(1), requestDurationData.DataPoints[0].Count)
			assertAttributes(t, requestDurationData.DataPoints[0].Attributes, map[string]string{
				observability.MetricAttrTransport: "http",
				observability.MetricAttrOperation: tt.expectedOperation,
				observability.MetricAttrStatus:    tt.expectedStatus,
				observability.MetricAttrOutcome:   tt.expectedOutcome,
			})
		})
	}
}

func TestRequestLoggerEmitsTransportFailureStatuses(t *testing.T) {
	tests := []struct {
		name           string
		buildRequest   func() *http.Request
		expectedStatus string
	}{
		{
			name: "cancelled context",
			buildRequest: func() *http.Request {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return httptest.NewRequest(http.MethodGet, "/products/search", nil).WithContext(ctx)
			},
			expectedStatus: "cancelled",
		},
		{
			name: "deadline exceeded context",
			buildRequest: func() *http.Request {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)

				return httptest.NewRequest(http.MethodGet, "/products/search", nil).WithContext(ctx)
			},
			expectedStatus: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

			previous := otel.GetMeterProvider()
			otel.SetMeterProvider(meterProvider)
			t.Cleanup(func() {
				require.NoError(t, meterProvider.Shutdown(context.Background()))
				otel.SetMeterProvider(previous)
			})

			provider := NewMiddlewaresProvider("test-service", slog.Default(), noop.NewTracerProvider().Tracer("test"))
			handler := provider.RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, tt.buildRequest())

			var rm metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &rm))

			requestTotal := findMetric(t, rm, observability.MetricNameRequestTotal)
			requestTotalData, ok := requestTotal.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, requestTotalData.DataPoints, 1)
			assertAttributes(t, requestTotalData.DataPoints[0].Attributes, map[string]string{
				observability.MetricAttrTransport: "http",
				observability.MetricAttrOperation: "/products/search",
				observability.MetricAttrStatus:    tt.expectedStatus,
				observability.MetricAttrOutcome:   "error",
			})
		})
	}
}

func TestRequestLoggerEmitsMetricsOnPanic(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
		otel.SetMeterProvider(previous)
	})

	provider := NewMiddlewaresProvider("test-service", slog.Default(), noop.NewTracerProvider().Tracer("test"))
	handler := provider.Recovery(provider.RequestLogger(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	req := httptest.NewRequest(http.MethodGet, "/v1/orders/123/items", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	requestTotal := findMetric(t, rm, observability.MetricNameRequestTotal)
	requestTotalData, ok := requestTotal.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, requestTotalData.DataPoints, 1)
	require.Equal(t, int64(1), requestTotalData.DataPoints[0].Value)
	assertAttributes(t, requestTotalData.DataPoints[0].Attributes, map[string]string{
		observability.MetricAttrTransport: "http",
		observability.MetricAttrOperation: "/v1/orders/{id}/items",
		observability.MetricAttrStatus:    "network_error",
		observability.MetricAttrOutcome:   "error",
	})
}

func setW3CPropagator(t *testing.T) {
	t.Helper()

	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		otel.SetTextMapPropagator(previous)
	})
}

func spanAttributes(span sdktrace.ReadOnlySpan) []attribute.KeyValue {
	return span.Attributes()
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	for _, scope := range rm.ScopeMetrics {
		for _, metricValue := range scope.Metrics {
			if metricValue.Name == name {
				return metricValue
			}
		}
	}

	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertAttributes(t *testing.T, attrs attribute.Set, expected map[string]string) {
	t.Helper()

	require.Equal(t, len(expected), attrs.Len())

	for key, expectedValue := range expected {
		value, ok := attrs.Value(attribute.Key(key))
		require.True(t, ok, "missing attribute %q", key)
		require.Equal(t, expectedValue, value.AsString())
	}
}
