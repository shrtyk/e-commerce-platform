package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres"
	identityapp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/app"
	config "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/config"
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

	if err := app.Run(ctx); err != nil {
		panic(fmt.Errorf("run app: %w", err))
	}
}
