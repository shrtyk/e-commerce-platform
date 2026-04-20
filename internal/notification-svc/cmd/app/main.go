package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres"
	notificationapp "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/config"
)

func main() {
	cfg := config.MustLoad()

	logger, err := logging.New(
		logging.EnvFromCfg(cfg.Service.Environment),
		logging.LogLevelFromCfg(cfg.LogLevel),
	)
	if err != nil {
		panic(fmt.Errorf("create logger: %w", err))
	}
	slog.SetDefault(logger)

	observability.InitPropagator()
	tracerProvider := observability.MustCreateTracerProvider(cfg.OTel, cfg.Service.Name)
	meterProvider := observability.MustCreateMeterProvider(cfg.OTel, cfg.Service.Name)
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	tracer := tracerProvider.Tracer(cfg.Service.Name)

	db := adapterpostgres.MustCreatePostgres(cfg.Postgres, cfg.Timeouts)
	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, db, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, tracer)

	app := notificationapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		notificationapp.WithLogger(logger),
		notificationapp.WithTracerProvider(tracerProvider),
		notificationapp.WithMeterProvider(meterProvider),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
