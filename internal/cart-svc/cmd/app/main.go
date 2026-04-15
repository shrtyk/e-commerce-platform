package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	redislib "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres"
	adapterredis "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/redis"
	cartapp "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
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
	catalogConn, err := grpcpkg.NewClient(
		cfg.Catalog.GRPCAddr,
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Errorf("create catalog grpc client: %w", err))
	}

	var redisClient *redislib.Client
	if cfg.Redis.Enabled {
		redisClient = adapterredis.MustCreateRedis(cfg.Redis, cfg.Timeouts)
	}

	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, nil, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, nil, tracer)

	app := cartapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		redisClient,
		cartapp.WithCatalogConn(catalogConn),
		cartapp.WithLogger(logger),
		cartapp.WithTracerProvider(tracerProvider),
		cartapp.WithMeterProvider(meterProvider),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
