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
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/jwt"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/password/bcrypt"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/repos"
	identityapp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/app"
	config "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
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
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
	)
	defer cancel()

	txProvider := sqltx.NewProvider(db, func(tx *sql.Tx) auth.IdentityRepos {
		return auth.IdentityRepos{
			Users:    repos.NewUserRepositoryFromTx(tx),
			Sessions: repos.NewSessionRepositoryFromTx(tx),
		}
	})

	authService := auth.NewAuthService(
		repos.NewUserRepository(db),
		repos.NewSessionRepository(db),
		txProvider,
		bcrypt.NewHasher(0),
		jwt.NewTokenIssuer(cfg.Auth.AccessTokenIssuer, cfg.Auth.AccessTokenKey, cfg.Auth.AccessTokenTTL),
		cfg.Auth.SessionTTL,
	)
	if cfg.Bootstrap.Enabled {
		var displayName *string
		if cfg.Bootstrap.DisplayName != "" {
			displayName = &cfg.Bootstrap.DisplayName
		}

		if err := authService.EnsureBootstrapAdmin(ctx, auth.BootstrapAdminInput{
			Email:       cfg.Bootstrap.Email,
			Password:    cfg.Bootstrap.Password,
			DisplayName: displayName,
		}); err != nil {
			panic(fmt.Errorf("bootstrap admin: %w", err))
		}
	}
	tokenVerifier := jwt.NewTokenVerifier(cfg.Auth.AccessTokenKey, cfg.Auth.AccessTokenIssuer)

	handler := adapterhttp.NewRouter(
		logger,
		cfg.Service.Name,
		authService,
		tokenVerifier,
		tracer,
	)
	grpcServer := adaptergrpc.NewServer(logger, cfg.Service.Name, authService, tokenVerifier, tracer)

	app := identityapp.NewApplication(
		&cfg,
		handler,
		grpcServer,
		db,
		identityapp.WithLogger(logger),
		identityapp.WithTracerProvider(tracerProvider),
		identityapp.WithMeterProvider(meterProvider),
	)

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
