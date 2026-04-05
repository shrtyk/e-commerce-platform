package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	commonconfig "github.com/shrtyk/e-commerce-platform/internal/common/config"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http"
	adapterpostgres "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres"
	identityapp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/app"
)

func main() {
	cfg := commonconfig.MustLoad()
	db := adapterpostgres.MustCreatePostgres(cfg)
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
