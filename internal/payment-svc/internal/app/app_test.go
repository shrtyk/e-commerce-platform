package app_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	grpcpkg "google.golang.org/grpc"

	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
	paymentapp "github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/app"
	"github.com/shrtyk/e-commerce-platform/internal/payment-svc/internal/config"
)

func TestApplicationRunValidation(t *testing.T) {
	tests := []struct {
		name    string
		app     *paymentapp.Application
		errWant error
	}{
		{
			name:    "missing config",
			app:     paymentapp.NewApplication(nil, nil, grpcpkg.NewServer(), nil),
			errWant: paymentapp.ErrConfigRequired,
		},
		{
			name: "missing grpc",
			app: paymentapp.NewApplication(
				&config.Config{Config: commoncfg.Config{Service: commoncfg.Service{Name: "payment"}}},
				nil,
				nil,
				nil,
			),
			errWant: paymentapp.ErrGRPCServerRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.app.Run(context.Background())
			require.ErrorIs(t, err, tt.errWant)
		})
	}
}

func TestApplicationRunCancelStopsServers(t *testing.T) {
	httpAddr := mustReserveTCPAddr(t)
	grpcAddr := mustReserveTCPAddr(t)

	app := paymentapp.NewApplication(
		newTestConfig(httpAddr, grpcAddr),
		http.NotFoundHandler(),
		grpcpkg.NewServer(),
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(ctx)
	}()

	require.NoError(t, waitForPort(httpAddr, 2*time.Second))
	require.NoError(t, waitForPort(grpcAddr, 2*time.Second))

	cancel()

	require.NoError(t, waitRunResult(errCh, 2*time.Second))
	require.NoError(t, assertPortReusable(httpAddr))
	require.NoError(t, assertPortReusable(grpcAddr))
}

func TestApplicationRunReturnsGRPCStartupError(t *testing.T) {
	httpAddr := mustReserveTCPAddr(t)

	app := paymentapp.NewApplication(
		newTestConfig(httpAddr, "bad-grpc-addr"),
		http.NotFoundHandler(),
		grpcpkg.NewServer(),
		nil,
	)

	err := app.Run(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "listen grpc")
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation error: %v", err)
	}
}

func newTestConfig(httpAddr, grpcAddr string) *config.Config {
	return &config.Config{
		Config: commoncfg.Config{
			Service: commoncfg.Service{
				Name:     "payment",
				HTTPAddr: httpAddr,
				GRPCAddr: grpcAddr,
			},
			Timeouts: commoncfg.Timeouts{
				Shutdown: 500 * time.Millisecond,
			},
			HTTPTimeouts: commoncfg.HTTPTimeouts{
				ReadHeader: 100 * time.Millisecond,
				Read:       time.Second,
				Write:      time.Second,
				Idle:       time.Second,
			},
		},
	}
}

func mustReserveTCPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

func waitForPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("wait for port %s: %w", addr, err)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func waitRunResult(errCh <-chan error, timeout time.Duration) error {
	select {
	case err := <-errCh:
		return err
	case <-time.After(timeout):
		return errors.New("application run timeout")
	}
}

func assertPortReusable(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	if closeErr := listener.Close(); closeErr != nil {
		return fmt.Errorf("close listener on %s: %w", addr, closeErr)
	}

	return nil
}
