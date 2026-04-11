package observability

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/sdk/metric"
	trace "go.opentelemetry.io/otel/sdk/trace"
)

func Shutdown(ctx context.Context, tracer *trace.TracerProvider, meter *metric.MeterProvider) error {
	var err error

	if tracer != nil {
		err = errors.Join(err, tracer.Shutdown(ctx))
	}

	if meter != nil {
		err = errors.Join(err, meter.Shutdown(ctx))
	}

	return err
}
