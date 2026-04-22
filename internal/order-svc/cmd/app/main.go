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
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http"
	adapterinboundkafka "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/kafka"
	adaptercheckoutgrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/checkout/grpc"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/events"
	adapterkafka "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/kafka"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/outbox"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/repos"
	orderapp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
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
	orderRepository := adapterpostgresrepos.NewOrderRepository(db)
	sagaRepository := adapterpostgresrepos.NewOrderSagaStateRepository(db)
	outboxRepo := adapteroutbox.NewRepository(db)
	outboxEventPublisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepo)

	kafkaBrokers := strings.Split(cfg.Kafka.Brokers, ",")
	kafkaClient, err := commonkafka.NewClient(kgo.SeedBrokers(kafkaBrokers...))
	if err != nil {
		panic(fmt.Errorf("create kafka client: %w", err))
	}
	defer kafkaClient.Close()

	var paymentEventsClient *commonkafka.Client
	if cfg.PaymentEvents.Enabled {
		paymentEventsClient, err = commonkafka.NewClient(
			kgo.SeedBrokers(kafkaBrokers...),
			kgo.ConsumerGroup(cfg.PaymentEvents.GroupID),
			kgo.ConsumeTopics(cfg.PaymentEvents.Topic),
		)
		if err != nil {
			panic(fmt.Errorf("create payment events kafka client: %w", err))
		}
		defer paymentEventsClient.Close()
	}

	schemaRegistryClient, err := sr.NewClient(sr.URLs(cfg.SchemaRegistry.URL))
	if err != nil {
		panic(fmt.Errorf("create schema registry client: %w", err))
	}

	typeRegistry := commonkafka.NewTypeRegistry()
	err = typeRegistry.RegisterMessages(&orderv1.OrderCreated{}, &orderv1.OrderCancelled{}, &orderv1.OrderConfirmed{})
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

	txProvider := sqltx.NewProvider(db, func(sqlTx *sql.Tx) checkout.TransactionRepos {
		return checkout.TransactionRepos{
			Orders:    adapterpostgresrepos.NewOrderRepositoryFromTx(sqlTx),
			Saga:      adapterpostgresrepos.NewOrderSagaStateRepositoryFromTx(sqlTx),
			Publisher: adapterevents.MustCreateOutboxEventPublisher(adapteroutbox.NewRepositoryFromTx(sqlTx)),
		}
	})

	cartGRPCAddr := strings.TrimSpace(os.Getenv("CART_GRPC_ADDR"))
	if cartGRPCAddr == "" {
		cartGRPCAddr = "cart-svc:9090"
	}

	catalogGRPCAddr := strings.TrimSpace(os.Getenv("CATALOG_GRPC_ADDR"))
	if catalogGRPCAddr == "" {
		catalogGRPCAddr = "product-svc:9090"
	}

	paymentGRPCAddr := strings.TrimSpace(os.Getenv("PAYMENT_GRPC_ADDR"))
	if paymentGRPCAddr == "" {
		paymentGRPCAddr = "payment-svc:9090"
	}

	cartConn, err := grpcpkg.NewClient(
		cartGRPCAddr,
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Errorf("create cart grpc client: %w", err))
	}

	catalogConn, err := grpcpkg.NewClient(
		catalogGRPCAddr,
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Errorf("create catalog grpc client: %w", err))
	}

	paymentConn, err := grpcpkg.NewClient(
		paymentGRPCAddr,
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Errorf("create payment grpc client: %w", err))
	}

	catalogClient := catalogv1.NewCatalogServiceClient(catalogConn)
	checkoutService := checkout.NewService(
		orderRepository,
		sagaRepository,
		adaptercheckoutgrpc.NewCheckoutSnapshotRepository(
			cartv1.NewCartServiceClient(cartConn),
			catalogClient,
		),
		adaptercheckoutgrpc.NewStockReservationService(catalogClient),
		adaptercheckoutgrpc.NewStockReleaseService(catalogClient),
		adaptercheckoutgrpc.NewCheckoutPaymentService(paymentv1.NewPaymentServiceClient(paymentConn)),
		checkout.NewCheckoutIdempotencyGuard(orderRepository),
	).WithEventing(outboxEventPublisher, txProvider, cfg.Service.Name)

	authAccessTokenKey := strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_KEY"))
	if authAccessTokenKey == "" {
		panic("AUTH_ACCESS_TOKEN_KEY is required")
	}

	authAccessTokenIssuer := strings.TrimSpace(os.Getenv("AUTH_ACCESS_TOKEN_ISSUER"))
	if authAccessTokenIssuer == "" {
		panic("AUTH_ACCESS_TOKEN_ISSUER is required")
	}

	tokenVerifier := commonjwt.NewTokenVerifier(authAccessTokenKey, authAccessTokenIssuer)

	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, db, checkoutService, tokenVerifier, tracer)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, checkoutService, tokenVerifier, tracer)

	var paymentEventsWorker *adapterinboundkafka.PaymentEventsWorker
	if cfg.PaymentEvents.Enabled {
		paymentSerde := commonkafka.NewProtoSerde(schemaRegistryClient, commonkafka.NewDescriptorSchemaProvider())
		if err := paymentSerde.RegisterTypes(
			context.Background(),
			cfg.PaymentEvents.Topic,
			&paymentv1.PaymentInitiated{},
			&paymentv1.PaymentSucceeded{},
			&paymentv1.PaymentFailed{},
		); err != nil {
			panic(fmt.Errorf("register payment event schemas: %w", err))
		}

		paymentConsumer, err := commonkafka.NewConsumer(paymentEventsClient, paymentSerde)
		if err != nil {
			panic(fmt.Errorf("create payment events consumer: %w", err))
		}

		paymentEventsWorker, err = adapterinboundkafka.NewPaymentEventsWorker(
			paymentConsumer,
			checkoutService,
			adapterinboundkafka.PaymentEventsWorkerConfig{PollInterval: cfg.PaymentEvents.PollInterval},
		)
		if err != nil {
			panic(fmt.Errorf("create payment events worker: %w", err))
		}
	}

	app := orderapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		orderapp.WithLogger(logger),
		orderapp.WithTracerProvider(tracerProvider),
		orderapp.WithMeterProvider(meterProvider),
		orderapp.WithBackgroundWorker(relayWorker),
		orderapp.WithBackgroundWorker(paymentEventsWorker),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	defer func() {
		if closeErr := cartConn.Close(); closeErr != nil {
			logger.Error("close cart grpc conn", "error", closeErr.Error())
		}
		if closeErr := catalogConn.Close(); closeErr != nil {
			logger.Error("close catalog grpc conn", "error", closeErr.Error())
		}
		if closeErr := paymentConn.Close(); closeErr != nil {
			logger.Error("close payment grpc conn", "error", closeErr.Error())
		}
	}()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
