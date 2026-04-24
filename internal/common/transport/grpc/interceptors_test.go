package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryLogging(t *testing.T) {
	tests := []struct {
		name          string
		requestID     string
		setRequestID  bool
		handler       grpcpkg.UnaryHandler
		wantStatus    codes.Code
		checkDuration func(*testing.T, map[string]interface{})
	}{
		{
			name:         "logs fields for successful response",
			requestID:    "req-123",
			setRequestID: true,
			handler: func(ctx context.Context, req interface{}) (interface{}, error) {
				return "ok", nil
			},
			wantStatus: codes.OK,
			checkDuration: func(t *testing.T, entry map[string]interface{}) {
				duration, ok := entry["duration_ms"].(float64)
				require.True(t, ok)
				require.GreaterOrEqual(t, duration, float64(0))
			},
		},
		{
			name:         "logs duration and grpc status from error",
			requestID:    "req-456",
			setRequestID: true,
			handler: func(ctx context.Context, req interface{}) (interface{}, error) {
				time.Sleep(5 * time.Millisecond)
				return nil, status.Error(codes.NotFound, "missing")
			},
			wantStatus: codes.NotFound,
			checkDuration: func(t *testing.T, entry map[string]interface{}) {
				duration, ok := entry["duration_ms"].(float64)
				require.True(t, ok)
				require.GreaterOrEqual(t, duration, float64(5))
			},
		},
		{
			name:         "logs empty request id when missing from context",
			setRequestID: false,
			handler: func(ctx context.Context, req interface{}) (interface{}, error) {
				return "ok", nil
			},
			wantStatus: codes.OK,
			checkDuration: func(t *testing.T, entry map[string]interface{}) {
				duration, ok := entry["duration_ms"].(float64)
				require.True(t, ok)
				require.GreaterOrEqual(t, duration, float64(0))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			provider := NewInterceptorsProvider("identity-svc", logger)
			interceptor := provider.UnaryLogging()

			ctx := context.Background()
			if tt.setRequestID {
				ctx = transport.WithRequestID(ctx, tt.requestID)
			}
			info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

			resp, err := interceptor(ctx, "request", info, tt.handler)
			if tt.wantStatus == codes.OK {
				require.NoError(t, err)
				require.Equal(t, "ok", resp)
			} else {
				require.Error(t, err)
				require.Equal(t, tt.wantStatus, status.Code(err))
			}

			var entry map[string]interface{}
			err = json.Unmarshal(buf.Bytes(), &entry)
			require.NoError(t, err)

			require.Equal(t, "request", entry["msg"])
			require.Equal(t, "identity-svc", entry["service"])
			_, methodExists := entry["method"]
			require.False(t, methodExists)
			require.Equal(t, "/identity.v1.AuthService/Login", entry["path"])
			require.Equal(t, float64(tt.wantStatus), entry["status"])
			require.Equal(t, tt.wantStatus.String(), entry["grpc_status"])
			require.Equal(t, tt.requestID, entry["request_id"])
			tt.checkDuration(t, entry)
		})
	}
}

func TestUnaryRecoveryReturnsInternalOnPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	provider := NewInterceptorsProvider("identity-svc", logger)
	interceptor := provider.UnaryRecovery()

	ctx := transport.WithRequestID(context.Background(), "req-panic")
	info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

	resp, err := interceptor(ctx, "request", info, func(context.Context, interface{}) (interface{}, error) {
		panic("boom")
	})

	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal server error", status.Convert(err).Message())
}

func TestUnaryRecoveryLogsStackTrace(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	provider := NewInterceptorsProvider("identity-svc", logger)
	interceptor := provider.UnaryRecovery()

	ctx := transport.WithRequestID(context.Background(), "req-stack")
	info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

	_, err := interceptor(ctx, "request", info, func(context.Context, interface{}) (interface{}, error) {
		panic(errors.New("panic value"))
	})
	require.Error(t, err)

	var entry map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err)

	require.Equal(t, "panic recovered", entry["msg"])
	require.Equal(t, "panic value", entry["panic"])
	require.Equal(t, "req-stack", entry["request_id"])

	stack, ok := entry["stack"].(string)
	require.True(t, ok)
	require.NotEmpty(t, stack)
	require.True(t, strings.Contains(stack, "goroutine"))
}

func TestUnaryRecoveryPassesThroughHandlerResponse(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	provider := NewInterceptorsProvider("identity-svc", logger)
	interceptor := provider.UnaryRecovery()

	ctx := context.Background()
	info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

	wantResp := "ok"
	resp, err := interceptor(ctx, "request", info, func(context.Context, interface{}) (interface{}, error) {
		return wantResp, nil
	})

	require.NoError(t, err)
	require.Equal(t, wantResp, resp)
}

func TestUnaryTracing(t *testing.T) {
	tests := []struct {
		name      string
		handler   grpcpkg.UnaryHandler
		wantError bool
	}{
		{
			name: "creates span and propagates context",
			handler: func(ctx context.Context, req interface{}) (interface{}, error) {
				span := trace.SpanFromContext(ctx)
				require.True(t, span.SpanContext().IsValid())
				return "ok", nil
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spanRecorder := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
			tracer := tracerProvider.Tracer("test-grpc-tracer")

			provider := NewInterceptorsProviderWithTracer("identity-svc", slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), tracer)
			interceptor := provider.UnaryTracing()
			info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

			resp, err := interceptor(context.Background(), "request", info, tt.handler)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "ok", resp)
			}

			spans := spanRecorder.Ended()
			require.Len(t, spans, 1)

			span := spans[0]
			require.Equal(t, "/identity.v1.AuthService/Login", span.Name())
			require.Contains(t, spanAttributes(span), attribute.String("rpc.system", "grpc"))
			require.Contains(t, spanAttributes(span), attribute.String("rpc.service", "identity.v1.AuthService"))
			require.Contains(t, spanAttributes(span), attribute.String("rpc.method", "Login"))
		})
	}
}

func TestUnaryTracingExtractsRemoteParentContextFromMetadata(t *testing.T) {
	setW3CPropagator(t)

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test-grpc-tracer")

	provider := NewInterceptorsProviderWithTracer("identity-svc", slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), tracer)
	interceptor := provider.UnaryTracing()

	parentSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3},
		SpanID:     trace.SpanID{4, 4, 4, 4, 4, 4, 4, 4},
		TraceFlags: trace.FlagsSampled,
	})

	md := observability.InjectGRPCMetadata(trace.ContextWithSpanContext(context.Background(), parentSpanContext), nil)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpcpkg.UnaryServerInfo{FullMethod: "/identity.v1.AuthService/Login"}

	_, err := interceptor(ctx, "request", info, func(ctx context.Context, _ interface{}) (interface{}, error) {
		span := trace.SpanFromContext(ctx)
		require.True(t, span.SpanContext().IsValid())
		return "ok", nil
	})
	require.NoError(t, err)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	require.Equal(t, parentSpanContext.TraceID(), span.Parent().TraceID())
	require.Equal(t, parentSpanContext.SpanID(), span.Parent().SpanID())
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
