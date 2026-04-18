package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"go.opentelemetry.io/otel"

	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/events"
	adapterkafka "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/kafka"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/repos"
	productapp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/config"
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
	kafkaBrokers := strings.Split(cfg.Kafka.Brokers, ",")
	kafkaClient, err := commonkafka.NewClient(kgo.SeedBrokers(kafkaBrokers...))
	if err != nil {
		panic(fmt.Errorf("create kafka client: %w", err))
	}
	defer kafkaClient.Close()

	schemaRegistryClient, err := sr.NewClient(sr.URLs(cfg.SchemaRegistry.URL))
	if err != nil {
		panic(fmt.Errorf("create schema registry client: %w", err))
	}

	productsRepo := repos.NewProductRepository(db)
	stocksRepo := repos.NewStockRepository(db)
	outboxRepo := adapteroutbox.NewRepository(db)
	outboxEventPublisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepo)
	typeRegistry := commonkafka.NewTypeRegistry()
	err = typeRegistry.RegisterMessages(&catalogv1.ProductCreated{})
	if err != nil {
		panic(fmt.Errorf("register kafka type: %w", err))
	}

	relayPublisher, err := adapterkafka.NewPublisher(kafkaClient, schemaRegistryClient, typeRegistry)
	if err != nil {
		panic(fmt.Errorf("create relay kafka publisher: %w", err))
	}

	relayWorker, err := adapterkafka.NewRelayWorker(outboxRepo, relayPublisher, adapterkafka.RelayConfig{
		BatchSize:        cfg.Relay.BatchSize,
		Interval:         cfg.Relay.Interval,
		RetryBaseBackoff: cfg.Relay.RetryBaseBackoff,
		RetryMaxBackoff:  cfg.Relay.RetryMaxBackoff,
		WorkerID:         cfg.Relay.WorkerID,
		StaleLockTTL:     cfg.Relay.StaleLockTTL,
	})
	if err != nil {
		panic(fmt.Errorf("create outbox relay worker: %w", err))
	}

	txProvider := sqltx.NewProvider(db, func(tx *sql.Tx) catalog.CatalogRepos {
		return catalog.CatalogRepos{
			Products:  repos.NewProductRepositoryFromTx(tx),
			Stocks:    repos.NewStockRepositoryFromTx(tx),
			Publisher: adapterevents.MustCreateOutboxEventPublisher(adapteroutbox.NewRepositoryFromTx(tx)),
		}
	})

	catalogService := catalog.NewCatalogService(productsRepo, stocksRepo, outboxEventPublisher, txProvider, cfg.Service.Name)
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
		productapp.WithBackgroundWorker(relayWorker),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
