package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.opentelemetry.io/otel"

	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres"
	orderapp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/config"
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

	authAccessTokenKey := strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_KEY"))
	if authAccessTokenKey == "" {
		panic("AUTH_ACCESS_TOKEN_KEY is required")
	}

	authAccessTokenIssuer := strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_ISSUER"))
	if authAccessTokenIssuer == "" {
		panic("AUTH_ACCESS_TOKEN_ISSUER is required")
	}

	tokenVerifier := commonjwt.NewTokenVerifier(authAccessTokenKey, authAccessTokenIssuer)

	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, db, tokenVerifier, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, tokenVerifier, tracer)

	app := orderapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		orderapp.WithLogger(logger),
		orderapp.WithTracerProvider(tracerProvider),
		orderapp.WithMeterProvider(meterProvider),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
