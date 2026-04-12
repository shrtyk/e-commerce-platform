package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/otel"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/repos"
	productapp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
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
	tracer := tracerProvider.Tracer(cfg.Service.Name)

	db := adapterpostgres.MustCreatePostgres(cfg.Postgres, cfg.Timeouts)

	productsRepo := repos.NewProductRepository(db)
	stocksRepo := repos.NewStockRepository(db)
	publisher := noopEventPublisher{}

	txProvider := sqltx.NewProvider(db, func(tx *sql.Tx) catalog.CatalogRepos {
		return catalog.CatalogRepos{
			Products:  repos.NewProductRepositoryFromTx(tx),
			Stocks:    repos.NewStockRepositoryFromTx(tx),
			Publisher: publisher,
		}
	})

	catalogService := catalog.NewCatalogService(productsRepo, stocksRepo, publisher, txProvider)
	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, catalogService, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, catalogService, tracer)

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
		panic(fmt.Errorf("run app: %w", err))
	}
}

type noopEventPublisher struct{}

func (noopEventPublisher) Publish(context.Context, domain.DomainEvent) error {
	return nil
}
