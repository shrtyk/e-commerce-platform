package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func MustCreateMeterProvider(cfg config.OTel, serviceName string) *metric.MeterProvider {
	options := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		options = append(options, otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	exporter, err := otlpmetricgrpc.New(context.Background(), options...)
	if err != nil {
		panic(fmt.Errorf("create otlp metric exporter: %w", err))
	}

	return metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exporter, metric.WithInterval(15*time.Second))),
		metric.WithResource(newResource(serviceName)),
	)
}
