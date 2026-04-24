package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"go.opentelemetry.io/otel"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/inbound/http"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/events"
	adapterkafka "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/kafka"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/outbox"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/postgres/repos"
	adapterprovider "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/adapters/outbound/provider"
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
	stubPaymentProvider := adapterprovider.NewStubProvider()

	relayWorker, relayKafkaClient, err := createRelayWorker(cfg, outboxRepository)
	if err != nil {
		panic(err)
	}
	if relayKafkaClient != nil {
		defer relayKafkaClient.Close()
	}

	paymentService := payment.NewService(paymentAttemptRepository, outboxEventPublisher, stubPaymentProvider, cfg.Service.Name).
		WithEventsTopic(cfg.Events.Topic)
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
		paymentapp.WithBackgroundWorker(relayWorker),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}

func createRelayWorker(cfg config.Config, outboxRepository *adapteroutbox.Repository) (*adapterkafka.RelayWorker, *commonkafka.Client, error) {
	if !cfg.Relay.Enabled {
		slog.Info("outbox relay worker is disabled")
		return nil, nil, nil
	}

	kafkaBrokers := splitAndTrimKafkaBrokers(cfg.Kafka.Brokers)
	relayKafkaClient, err := commonkafka.NewClient(kgo.SeedBrokers(kafkaBrokers...))
	if err != nil {
		return nil, nil, fmt.Errorf("create relay kafka client: %w", err)
	}

	schemaRegistryClient, err := sr.NewClient(sr.URLs(cfg.SchemaRegistry.URL))
	if err != nil {
		relayKafkaClient.Close()
		return nil, nil, fmt.Errorf("create schema registry client: %w", err)
	}

	typeRegistry := commonkafka.NewTypeRegistry()
	err = typeRegistry.RegisterMessages(
		&paymentv1.PaymentInitiated{},
		&paymentv1.PaymentSucceeded{},
		&paymentv1.PaymentFailed{},
	)
	if err != nil {
		relayKafkaClient.Close()
		return nil, nil, fmt.Errorf("register payment kafka types: %w", err)
	}

	relayPublisher, err := adapterkafka.NewPublisher(relayKafkaClient, schemaRegistryClient, typeRegistry)
	if err != nil {
		relayKafkaClient.Close()
		return nil, nil, fmt.Errorf("create relay kafka publisher: %w", err)
	}

	relayWorker, err := adapterkafka.NewRelayWorker(outboxRepository, relayPublisher, adapterkafka.RelayConfig{
		BatchSize:        cfg.Relay.BatchSize,
		Interval:         cfg.Relay.Interval,
		RetryBaseBackoff: cfg.Relay.RetryBaseBackoff,
		RetryMaxBackoff:  cfg.Relay.RetryMaxBackoff,
		WorkerID:         cfg.Relay.WorkerID,
		StaleLockTTL:     cfg.Relay.StaleLockTTL,
	})
	if err != nil {
		relayKafkaClient.Close()
		return nil, nil, fmt.Errorf("create outbox relay worker: %w", err)
	}

	return relayWorker, relayKafkaClient, nil
}

func splitAndTrimKafkaBrokers(rawBrokers string) []string {
	parts := strings.Split(rawBrokers, ",")
	brokers := make([]string, 0, len(parts))

	for _, broker := range parts {
		trimmed := strings.TrimSpace(broker)
		if trimmed == "" {
			continue
		}

		brokers = append(brokers, trimmed)
	}

	return brokers
}
