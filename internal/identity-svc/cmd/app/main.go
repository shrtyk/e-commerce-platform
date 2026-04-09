package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shrtyk/e-commerce-platform/internal/common/logging"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
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

	db := adapterpostgres.MustCreatePostgres(cfg.Postgres)
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

	handler := adapterhttp.NewRouter(logger, cfg.Service.Name, authService)
	app := identityapp.NewApplication(&cfg, handler, db, identityapp.WithLogger(logger))

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
