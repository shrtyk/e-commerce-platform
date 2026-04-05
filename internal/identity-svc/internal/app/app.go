package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	commonconfig "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

type Application struct {
	Config   *commonconfig.Config
	Database *sql.DB
	Logger   *slog.Logger
	Handler  http.Handler
}

type option func(*Application)

var errConfigRequired = errors.New("app config is required")

func NewApplication(
	cfg *commonconfig.Config,
	handler http.Handler,
	db *sql.DB,
	opts ...option,
) *Application {
	app := &Application{
		Config:   cfg,
		Database: db,
		Handler:  handler,
	}

	for _, opt := range opts {
		opt(app)
	}

	if app.Logger == nil {
		app.Logger = slog.Default()
	}

	return app
}

func WithLogger(logger *slog.Logger) option {
	return func(a *Application) {
		a.Logger = logger
	}
}

func (a *Application) Run(ctx context.Context) error {
	if a.Config == nil || a.Config.Service.Name == "" {
		return errConfigRequired
	}

	if a.Handler == nil {
		a.Handler = http.NotFoundHandler()
	}

	if a.Database != nil {
		defer a.Database.Close()
	}

	return runHTTPServer(ctx, *a.Config, a.Handler)
}

func runHTTPServer(
	ctx context.Context,
	cfg commonconfig.Config,
	handler http.Handler,
) error {
	srv := &http.Server{
		Addr:              cfg.Service.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}

		return nil
	case err := <-errCh:
		return err
	}
}
