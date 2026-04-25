package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	grpcpkg "google.golang.org/grpc"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/config"
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
		defer func() {
			if err := a.Database.Close(); err != nil {
				a.Logger.Warn("close database", "error", err)
			}
		}()
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
	defer func() {
		if err := grpcListener.Close(); err != nil {
			a.Logger.Warn("close grpc listener", "error", err)
		}
	}()

	serverErrCh := make(chan error, len(a.Workers)+2)
	var wg sync.WaitGroup
	wg.Add(len(a.Workers) + 2)

	go func() {
		defer wg.Done()

		serveErr := httpServer.ListenAndServe()
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrCh <- fmt.Errorf("serve http: %w", serveErr)
			return
		}

		serverErrCh <- nil
	}()

	for i, backgroundWorker := range a.Workers {
		idx := i
		workerInstance := backgroundWorker

		go func() {
			defer wg.Done()

			if runErr := workerInstance.Run(ctx); runErr != nil {
				if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
					serverErrCh <- nil
					return
				}

				serverErrCh <- fmt.Errorf("run background worker %d: %w", idx, runErr)
				return
			}

			serverErrCh <- nil
		}()
	}

	go func() {
		defer wg.Done()

		serveErr := a.GRPCServer.Serve(grpcListener)
		if serveErr != nil && !errors.Is(serveErr, grpcpkg.ErrServerStopped) {
			serverErrCh <- fmt.Errorf("serve grpc: %w", serveErr)
			return
		}

		serverErrCh <- nil
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case serveErr := <-serverErrCh:
		runErr = serveErr
	}

	shutdownTimeout := a.Config.Timeouts.Shutdown

	httpShutdownCtx, cancelHTTP := context.WithTimeout(context.Background(), shutdownTimeout)
	httpShutdownErr := httpServer.Shutdown(httpShutdownCtx)
	cancelHTTP()
	if httpShutdownErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("shutdown http: %w", httpShutdownErr))
	}

	stopped := make(chan struct{})
	go func() {
		a.GRPCServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		a.GRPCServer.Stop()
	}

	if a.TracerProvider != nil {
		tracerShutdownCtx, cancelTracer := context.WithTimeout(context.Background(), shutdownTimeout)
		tracerShutdownErr := a.TracerProvider.Shutdown(tracerShutdownCtx)
		cancelTracer()

		if tracerShutdownErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shutdown tracer provider: %w", tracerShutdownErr))
		}
	}

	if a.MeterProvider != nil {
		meterShutdownCtx, cancelMeter := context.WithTimeout(context.Background(), shutdownTimeout)
		meterShutdownErr := a.MeterProvider.Shutdown(meterShutdownCtx)
		cancelMeter()

		if meterShutdownErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shutdown meter provider: %w", meterShutdownErr))
		}
	}

	wg.Wait()
	close(serverErrCh)

	for serveErr := range serverErrCh {
		runErr = errors.Join(runErr, serveErr)
	}

	return runErr
}
