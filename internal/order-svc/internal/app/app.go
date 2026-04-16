package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/sync/errgroup"
	grpcpkg "google.golang.org/grpc"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/config"
)

type Application struct {
	Config         *config.Config
	Database       *sql.DB
	Logger         *slog.Logger
	Handler        http.Handler
	GRPCServer     *grpcpkg.Server
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
}

type option func(*Application)

var (
	ErrConfigRequired     = errors.New("app config is required")
	ErrGRPCServerRequired = errors.New("app grpc server is required")
)

func NewApplication(
	cfg *config.Config,
	handler http.Handler,
	grpcServer *grpcpkg.Server,
	db *sql.DB,
	opts ...option,
) *Application {
	app := &Application{
		Config:     cfg,
		Database:   db,
		Handler:    handler,
		GRPCServer: grpcServer,
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

func WithTracerProvider(provider *sdktrace.TracerProvider) option {
	return func(a *Application) {
		a.TracerProvider = provider
	}
}

func WithMeterProvider(provider *metric.MeterProvider) option {
	return func(a *Application) {
		a.MeterProvider = provider
	}
}

func (a *Application) Run(ctx context.Context) error {
	if a.Config == nil || a.Config.Service.Name == "" {
		return ErrConfigRequired
	}

	if a.Handler == nil {
		a.Handler = http.NotFoundHandler()
	}

	if a.GRPCServer == nil {
		return ErrGRPCServerRequired
	}

	if a.Database != nil {
		defer a.Database.Close()
	}

	g, runCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return runHTTPServer(runCtx, *a.Config, a.Handler)
	})

	g.Go(func() error {
		return runGRPCServer(runCtx, *a.Config, a.GRPCServer)
	})

	err := g.Wait()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.Timeouts.Shutdown)
	defer cancel()

	if a.TracerProvider != nil {
		if shutdownErr := a.TracerProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			if err != nil {
				return errors.Join(err, fmt.Errorf("shutdown tracer provider: %w", shutdownErr))
			}

			return fmt.Errorf("shutdown tracer provider: %w", shutdownErr)
		}
	}

	if a.MeterProvider != nil {
		if shutdownErr := a.MeterProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			if err != nil {
				return errors.Join(err, fmt.Errorf("shutdown meter provider: %w", shutdownErr))
			}

			return fmt.Errorf("shutdown meter provider: %w", shutdownErr)
		}
	}

	return err
}

func runHTTPServer(
	ctx context.Context,
	cfg config.Config,
	handler http.Handler,
) error {
	srv := &http.Server{
		Addr:              cfg.Service.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTPTimeouts.ReadHeader,
		ReadTimeout:       cfg.HTTPTimeouts.Read,
		WriteTimeout:      cfg.HTTPTimeouts.Write,
		IdleTimeout:       cfg.HTTPTimeouts.Idle,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve http: %w", err)
			return
		}

		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Timeouts.Shutdown)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http: %w", err)
		}

		return nil
	case err := <-errCh:
		return err
	}
}

func runGRPCServer(
	ctx context.Context,
	cfg config.Config,
	server *grpcpkg.Server,
) error {
	listener, err := net.Listen("tcp", cfg.Service.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil {
			errCh <- fmt.Errorf("serve grpc: %w", serveErr)
			return
		}

		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			return nil
		case <-time.After(cfg.Timeouts.Shutdown):
			server.Stop()
			return nil
		}
	case serveErr := <-errCh:
		return serveErr
	}
}
