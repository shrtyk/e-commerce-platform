package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/jwt"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/password/bcrypt"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/repos"
	identityapp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/app"
	config "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"

	txpostgres "github.com/shrtyk/e-commerce-platform/internal/common/tx/postgres"
)

func main() {
	cfg := config.MustLoad()
	db := adapterpostgres.MustCreatePostgres(cfg.Postgres)
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
	)
	defer cancel()

	handler := adapterhttp.NewRouter()
	app := identityapp.NewApplication(&cfg, handler, db)

	txProvider := txpostgres.NewProvider(db, func(tx *sql.Tx) auth.IdentityRepos {
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
	_ = authService

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
