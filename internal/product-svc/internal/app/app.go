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

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/config"
)

type Application struct {
	Config         *config.Config
	Database       *sql.DB
	Logger         *slog.Logger
	Handler        http.Handler
	GRPCServer     *grpcpkg.Server
	Workers        []worker
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *metric.MeterProvider
}

type worker interface {
	Run(ctx context.Context) error
}

type option func(*Application)

var (
	errConfigRequired     = errors.New("app config is required")
	errGRPCServerRequired = errors.New("app grpc server is required")
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

func WithBackgroundWorker(backgroundWorker worker) option {
	return func(a *Application) {
		if backgroundWorker == nil {
			return
		}

		a.Workers = append(a.Workers, backgroundWorker)
	}
}

func (a *Application) Run(ctx context.Context) error {
	if a.Config == nil || a.Config.Service.Name == "" {
		return errConfigRequired
	}

	if a.Handler == nil {
		a.Handler = http.NotFoundHandler()
	}

	if a.GRPCServer == nil {
		return errGRPCServerRequired
	}

	if a.Database != nil {
		defer a.Database.Close()
	}

	httpServer := &http.Server{
		Addr:              a.Config.Service.HTTPAddr,
		Handler:           a.Handler,
		ReadHeaderTimeout: a.Config.HTTPTimeouts.ReadHeader,
		ReadTimeout:       a.Config.HTTPTimeouts.Read,
		WriteTimeout:      a.Config.HTTPTimeouts.Write,
		IdleTimeout:       a.Config.HTTPTimeouts.Idle,
	}

	grpcListener, err := net.Listen("tcp", a.Config.Service.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	g := &errgroup.Group{}
	serveErrCh := make(chan error, len(a.Workers)+2)

	g.Go(func() error {
		err := serveHTTPServer(httpServer)
		if err != nil {
			serveErrCh <- err
		}

		return err
	})

	g.Go(func() error {
		err := serveGRPCServer(a.GRPCServer, grpcListener)
		if err != nil {
			serveErrCh <- err
		}

		return err
	})

	for i, backgroundWorker := range a.Workers {
		idx := i
		workerInstance := backgroundWorker

		g.Go(func() error {
			if runErr := workerInstance.Run(runCtx); runErr != nil {
				if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
					return nil
				}

				wrapped := fmt.Errorf("run background worker %d: %w", idx, runErr)
				serveErrCh <- wrapped
				return wrapped
			}

			return nil
		})
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- g.Wait()
	}()

	select {
	case <-runCtx.Done():
	case err = <-serveErrCh:
	}

	cancelRun()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.Timeouts.Shutdown)
	defer cancel()

	if shutdownErr := httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
		if err != nil {
			err = errors.Join(err, fmt.Errorf("shutdown http: %w", shutdownErr))
		} else {
			err = fmt.Errorf("shutdown http: %w", shutdownErr)
		}
	}

	stopped := make(chan struct{})
	go func() {
		a.GRPCServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(a.Config.Timeouts.Shutdown):
		a.GRPCServer.Stop()
	}

	if waitErr := <-waitErrCh; waitErr != nil {
		if err != nil {
			err = errors.Join(err, waitErr)
		} else {
			err = waitErr
		}
	}

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

func serveHTTPServer(server *http.Server) error {
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}

	return nil
}

func serveGRPCServer(server *grpcpkg.Server, listener net.Listener) error {
	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("serve grpc: %w", err)
	}

	return nil
}
