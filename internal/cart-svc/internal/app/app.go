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

	redislib "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	grpcpkg "google.golang.org/grpc"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/config"
)

type Application struct {
	Config         *config.Config
	Database       *sql.DB
	Redis          *redislib.Client
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
	redisClient *redislib.Client,
	opts ...option,
) *Application {
	app := &Application{
		Config:     cfg,
		Database:   db,
		Redis:      redisClient,
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

	if a.Redis != nil {
		defer a.Redis.Close()
	}

	httpServer, httpErrCh := runHTTPServer(*a.Config, a.Handler)
	grpcErrCh, grpcStop, err := runGRPCServer(*a.Config, a.GRPCServer)
	if err != nil {
		shutdownErr := shutdownHTTPServer(*a.Config, httpServer)
		if shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}

		return err
	}

	err = a.waitForStop(ctx, *a.Config, httpServer, httpErrCh, grpcErrCh, grpcStop)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.Timeouts.Shutdown)
	defer cancel()

	observabilityErr := shutdownProviders(shutdownCtx, err, a.TracerProvider, a.MeterProvider)
	if observabilityErr != nil {
		return observabilityErr
	}

	return err
}

func (a *Application) waitForStop(
	ctx context.Context,
	cfg config.Config,
	httpServer *http.Server,
	httpErrCh <-chan error,
	grpcErrCh <-chan error,
	grpcStop func(),
) error {
	var err error

	select {
	case <-ctx.Done():
		err = nil
	case serveErr := <-httpErrCh:
		err = serveErr
	case serveErr := <-grpcErrCh:
		err = serveErr
	}

	shutdownErr := shutdownInOrder(cfg, httpServer, grpcStop)
	if shutdownErr != nil {
		if err != nil {
			return errors.Join(err, shutdownErr)
		}

		return shutdownErr
	}

	return err
}

func shutdownProviders(
	shutdownCtx context.Context,
	err error,
	tracerProvider *sdktrace.TracerProvider,
	meterProvider *metric.MeterProvider,
) error {
	if tracerProvider != nil {
		if shutdownErr := tracerProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			if err != nil {
				return errors.Join(err, fmt.Errorf("shutdown tracer provider: %w", shutdownErr))
			}

			return fmt.Errorf("shutdown tracer provider: %w", shutdownErr)
		}
	}

	if meterProvider != nil {
		if shutdownErr := meterProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			if err != nil {
				return errors.Join(err, fmt.Errorf("shutdown meter provider: %w", shutdownErr))
			}

			return fmt.Errorf("shutdown meter provider: %w", shutdownErr)
		}
	}

	return err
}

func runHTTPServer(cfg config.Config, handler http.Handler) (*http.Server, <-chan error) {
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

	return srv, errCh
}

func runGRPCServer(cfg config.Config, server *grpcpkg.Server) (<-chan error, func(), error) {
	listener, err := net.Listen("tcp", cfg.Service.GRPCAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen grpc: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil {
			if errors.Is(serveErr, grpcpkg.ErrServerStopped) {
				errCh <- nil
				return
			}

			errCh <- fmt.Errorf("serve grpc: %w", serveErr)
			return
		}

		errCh <- nil
	}()

	return errCh, func() {
		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
		case <-time.After(cfg.Timeouts.Shutdown):
			server.Stop()
		}
	}, nil
}

func shutdownInOrder(cfg config.Config, httpServer *http.Server, grpcStop func()) error {
	httpErr := shutdownHTTPServer(cfg, httpServer)

	if grpcStop != nil {
		grpcStop()
	}

	return httpErr
}

func shutdownHTTPServer(cfg config.Config, srv *http.Server) error {
	if srv == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Timeouts.Shutdown)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http: %w", err)
	}

	return nil
}
