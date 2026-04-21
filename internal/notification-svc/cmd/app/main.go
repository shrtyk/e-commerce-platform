package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	"go.opentelemetry.io/otel"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	commonkafka "github.com/shrtyk/e-commerce-platform/internal/common/messaging/kafka"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/http"
	adapterinboundkafka "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/inbound/kafka"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/postgres/repos"
	adapterprovider "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/adapters/outbound/provider"
	notificationapp "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/service/notification"
)

type orderEventsConsumerWithCommit struct {
	consumer *commonkafka.Consumer
	client   *kgo.Client
}

func (c orderEventsConsumerWithCommit) Poll(ctx context.Context) ([]commonkafka.ConsumedMessage, error) {
	return c.consumer.Poll(ctx)
}

func (c orderEventsConsumerWithCommit) CommitUncommittedOffsets(ctx context.Context) error {
	return c.client.CommitUncommittedOffsets(ctx)
}

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
	deliveryRequestRepository := adapterpostgresrepos.NewDeliveryRequestRepository(db)
	deliveryAttemptRepository := adapterpostgresrepos.NewDeliveryAttemptRepository(db)
	consumerIdempotencyRepository := adapterpostgresrepos.NewConsumerIdempotencyRepository(db)
	notificationService := notification.NewNotificationService(
		deliveryRequestRepository,
		deliveryAttemptRepository,
		consumerIdempotencyRepository,
	).WithDeliveryProvider(adapterprovider.NewStubProvider())

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

	if cfg.OrderEvents.Enabled {
		kafkaBrokers := strings.Split(cfg.Kafka.Brokers, ",")
		orderEventsClient, err := kgo.NewClient(
			kgo.SeedBrokers(kafkaBrokers...),
			kgo.ConsumerGroup(cfg.OrderEvents.GroupID),
			kgo.ConsumeTopics(cfg.OrderEvents.Topic),
			kgo.DisableAutoCommit(),
		)
		if err != nil {
			panic(fmt.Errorf("create order events kafka client: %w", err))
		}
		defer orderEventsClient.Close()

		schemaRegistryClient, err := sr.NewClient(sr.URLs(cfg.SchemaRegistry.URL))
		if err != nil {
			panic(fmt.Errorf("create schema registry client: %w", err))
		}

		orderSerde := commonkafka.NewProtoSerde(schemaRegistryClient, commonkafka.NewDescriptorSchemaProvider())
		if err := orderSerde.RegisterType(context.Background(), cfg.OrderEvents.Topic, &orderv1.OrderConfirmed{}); err != nil {
			panic(fmt.Errorf("register order confirmed schema: %w", err))
		}
		if err := orderSerde.RegisterType(context.Background(), cfg.OrderEvents.Topic, &orderv1.OrderCancelled{}); err != nil {
			panic(fmt.Errorf("register order cancelled schema: %w", err))
		}

		orderEventsConsumer, err := commonkafka.NewConsumer(orderEventsClient, orderSerde)
		if err != nil {
			panic(fmt.Errorf("create order events consumer: %w", err))
		}

		orderEventsConsumerWithManualCommit := orderEventsConsumerWithCommit{
			consumer: orderEventsConsumer,
			client:   orderEventsClient,
		}

		orderEventsWorker, err := adapterinboundkafka.NewOrderEventsWorker(
			logger,
			orderEventsConsumerWithManualCommit,
			notificationService,
			adapterinboundkafka.OrderEventsWorkerConfig{
				PollInterval:      cfg.OrderEvents.PollInterval,
				ConsumerGroupName: cfg.OrderEvents.GroupID,
			},
		)
		if err != nil {
			panic(fmt.Errorf("create order events worker: %w", err))
		}

		notificationapp.WithBackgroundWorker(orderEventsWorker)(app)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
