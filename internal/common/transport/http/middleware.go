package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

type MiddlewaresProvider struct {
	serviceName   string
	logger        *slog.Logger
	tokenVerifier TokenVerifier
	tracer        trace.Tracer
}

func NewMiddlewaresProviderWithTracer(serviceName string, logger *slog.Logger, tracer trace.Tracer) *MiddlewaresProvider {
	return &MiddlewaresProvider{
		serviceName: serviceName,
		logger:      logger,
		tracer:      tracer,
	}
}

func NewMiddlewaresProvider(serviceName string, logger *slog.Logger, tracer trace.Tracer) *MiddlewaresProvider {
	return NewMiddlewaresProviderWithTracer(serviceName, logger, tracer)
}

func NewMiddlewaresProviderWithAuth(serviceName string, logger *slog.Logger, tokenVerifier TokenVerifier, tracer trace.Tracer) *MiddlewaresProvider {
	return &MiddlewaresProvider{
		serviceName:   serviceName,
		logger:        logger,
		tokenVerifier: tokenVerifier,
		tracer:        tracer,
	}
}

func (p *MiddlewaresProvider) RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}

		w.Header().Set("X-Request-ID", id)
		ctx := transport.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (p *MiddlewaresProvider) RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(ww, r)

		duration := time.Since(start).Milliseconds()
		requestID := w.Header().Get("X-Request-ID")
		if requestID == "" {
			requestID = transport.RequestIDFromContext(r.Context())
		}

		p.logger.Info("request",
			slog.String("service", p.serviceName),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.statusCode),
			slog.Int64("duration_ms", duration),
			slog.String("request_id", requestID),
		)
	})
}

func (p *MiddlewaresProvider) Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := w.Header().Get("X-Request-ID")
				if requestID == "" {
					requestID = transport.RequestIDFromContext(r.Context())
				}
				p.logger.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("request_id", requestID),
					slog.String("stack", string(debug.Stack())),
				)
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (p *MiddlewaresProvider) Tracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		ctx, span := p.tracer.Start(r.Context(), spanName, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		traceID := span.SpanContext().TraceID().String()
		if traceID != "00000000000000000000000000000000" {
			w.Header().Set("X-Trace-ID", traceID)
		}

		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(ww, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.status_code", ww.statusCode))
		if ww.statusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(ww.statusCode))
			return
		}

		span.SetStatus(codes.Ok, http.StatusText(ww.statusCode))
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
