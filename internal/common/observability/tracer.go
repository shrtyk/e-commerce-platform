package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	trace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func MustCreateTracerProvider(cfg config.OTel, serviceName string) *trace.TracerProvider {
	options := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		options = append(options, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	exporter, err := otlptracegrpc.New(context.Background(), options...)
	if err != nil {
		panic(fmt.Errorf("create otlp trace exporter: %w", err))
	}

	return trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(newResource(serviceName)),
	)
}

func newResource(serviceName string) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion("dev"),
		semconv.DeploymentEnvironment("local"),
	)
}
