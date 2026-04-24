package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

type MiddlewaresProvider struct {
	serviceName   string
	logger        *slog.Logger
	tokenVerifier TokenVerifier
	tracer        trace.Tracer
	requestMetrics *observability.RequestMetrics
}

func NewMiddlewaresProviderWithTracer(serviceName string, logger *slog.Logger, tracer trace.Tracer) *MiddlewaresProvider {
	requestMetrics := initRequestMetrics("internal/common/transport/http", logger)

	return &MiddlewaresProvider{
		serviceName: serviceName,
		logger:      logger,
		tracer:      tracer,
		requestMetrics: requestMetrics,
	}
}

func NewMiddlewaresProvider(serviceName string, logger *slog.Logger, tracer trace.Tracer) *MiddlewaresProvider {
	return NewMiddlewaresProviderWithTracer(serviceName, logger, tracer)
}

func NewMiddlewaresProviderWithAuth(serviceName string, logger *slog.Logger, tokenVerifier TokenVerifier, tracer trace.Tracer) *MiddlewaresProvider {
	requestMetrics := initRequestMetrics("internal/common/transport/http", logger)

	return &MiddlewaresProvider{
		serviceName:   serviceName,
		logger:        logger,
		tokenVerifier: tokenVerifier,
		tracer:        tracer,
		requestMetrics: requestMetrics,
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

		defer func() {
			recovered := recover()

			statusCode := ww.statusCode
			if recovered != nil {
				statusCode = http.StatusInternalServerError
				ww.statusCode = statusCode
			}

			duration := time.Since(start)
			requestID := w.Header().Get("X-Request-ID")
			if requestID == "" {
				requestID = transport.RequestIDFromContext(r.Context())
			}
			traceID := logging.TraceIDFromContext(r.Context())

			p.logger.Info("request", logging.RequestFields(
				p.serviceName,
				requestID,
				traceID,
				r.Method,
				r.URL.Path,
				statusCode,
				duration.Milliseconds(),
			)...)

			p.requestMetrics.Record(r.Context(), duration, observability.RequestMetricAttrs{
				Transport: "http",
				Operation: r.URL.Path,
				Status:    httpMetricStatus(statusCode, recovered, r.Context()),
				Outcome:   httpMetricOutcome(statusCode, recovered, r.Context()),
			})

			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(ww, r)
	})
}

func initRequestMetrics(meterName string, logger *slog.Logger) *observability.RequestMetrics {
	requestMetrics, err := observability.NewRequestMetrics(otel.GetMeterProvider().Meter(meterName))
	if err != nil {
		if logger != nil {
			logger.Error("metrics init failed",
				slog.String("component", "http.middleware"),
				slog.String("metric", observability.MetricNameRequestTotal),
				slog.String("error", err.Error()),
			)
		}

		return nil
	}

	return requestMetrics
}

func httpOutcome(statusCode int) string {
	if statusCode >= http.StatusBadRequest {
		return "error"
	}

	return "success"
}

func httpMetricStatus(statusCode int, recovered any, ctx context.Context) string {
	if recovered != nil {
		return "network_error"
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}

	return strconv.Itoa(statusCode)
}

func httpMetricOutcome(statusCode int, recovered any, ctx context.Context) string {
	if recovered != nil {
		return "error"
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
		return "error"
	}

	return httpOutcome(statusCode)
}

func (p *MiddlewaresProvider) Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID := w.Header().Get("X-Request-ID")
				if requestID == "" {
					requestID = transport.RequestIDFromContext(r.Context())
				}
				traceID := logging.TraceIDFromContext(r.Context())
				p.logger.Error("panic recovered", logging.PanicFields(
					p.serviceName,
					requestID,
					traceID,
					rec,
					string(debug.Stack()),
				)...)
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
		ctx := observability.ExtractHTTPHeaders(r.Context(), r.Header)
		ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		sc := span.SpanContext()
		if sc.IsValid() {
			w.Header().Set("X-Trace-ID", sc.TraceID().String())
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
