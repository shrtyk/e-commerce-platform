package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/inbound/http"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/events"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/outbox"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/repos"
	paymentapp "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/core/service/payment"
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
	paymentAttemptRepository := adapterpostgresrepos.NewPaymentAttemptRepository(db)
	outboxRepository := adapteroutbox.NewRepository(db)
	outboxEventPublisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepository)
	ensureRelaySafeMode(cfg)

	paymentService := payment.NewService(paymentAttemptRepository, outboxEventPublisher, cfg.Service.Name)
	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, db, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, paymentService, tracer)

	app := paymentapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		paymentapp.WithLogger(logger),
		paymentapp.WithTracerProvider(tracerProvider),
		paymentapp.WithMeterProvider(meterProvider),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}

func ensureRelaySafeMode(cfg config.Config) {
	if cfg.Relay.Enabled {
		panic("outbox relay is enabled, but Kafka publisher is not configured in Gate B")
	}

	slog.Info("outbox relay worker is disabled")
}
