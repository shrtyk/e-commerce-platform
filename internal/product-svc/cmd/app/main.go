package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres"
	productapp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/config"
)

func main() {
	cfg := config.MustLoad()

	logger, err := logging.New(
		logging.EnvFromCfg(cfg.Service.Environment),
		logging.LogLevelFromCfg(cfg.LogLevel),
	)
	if err != nil {
		panic(err)
	}
	slog.SetDefault(logger)

	observability.InitPropagator()
	tracerProvider := observability.MustCreateTracerProvider(cfg.OTel, cfg.Service.Name)
	meterProvider := observability.MustCreateMeterProvider(cfg.OTel, cfg.Service.Name)
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	db := adapterpostgres.MustCreatePostgres(cfg.Postgres, cfg.Timeouts)
	handler := http.NotFoundHandler()
	grpcServer := grpc.NewServer()

	app := productapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		productapp.WithLogger(logger),
		productapp.WithTracerProvider(tracerProvider),
		productapp.WithMeterProvider(meterProvider),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(err)
	}
}
