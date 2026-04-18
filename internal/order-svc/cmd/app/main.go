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
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/observability"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http"
	adaptercheckoutgrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/checkout/grpc"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres"
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
	)

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
