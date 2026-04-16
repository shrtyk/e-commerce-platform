package grpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

func TestNewServerRequiresTokenVerifier(t *testing.T) {
	require.PanicsWithValue(t, "grpc token verifier is required", func() {
		_ = NewServer(slog.Default(), "order-svc", nil, noop.NewTracerProvider().Tracer("test"))
	})
}

func TestNewServerWithTokenVerifier(t *testing.T) {
	server := NewServer(
		slog.Default(),
		"order-svc",
		tokenVerifierStub{},
		noop.NewTracerProvider().Tracer("test"),
	)

	require.NotNil(t, server)
	server.Stop()
}

func TestServerAuthInterceptorProtectsAllOrderRPCsWithoutAuthMetadata(t *testing.T) {
	h := newGRPCHarness(t)

	tests := []struct {
		name string
		call func(context.Context, orderv1.OrderServiceClient) error
	}{
		{
			name: "create order requires auth",
			call: func(ctx context.Context, client orderv1.OrderServiceClient) error {
				_, err := client.CreateOrder(ctx, &orderv1.CreateOrderRequest{})
				return err
			},
		},
		{
			name: "get order requires auth",
			call: func(ctx context.Context, client orderv1.OrderServiceClient) error {
				_, err := client.GetOrder(ctx, &orderv1.GetOrderRequest{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(context.Background(), h.client)
			require.Error(t, err)
			require.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}

	require.False(t, h.verifier.called)
}

type grpcHarness struct {
	client   orderv1.OrderServiceClient
	verifier *testTokenVerifier
}

func newGRPCHarness(t *testing.T) *grpcHarness {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := &testTokenVerifier{}
	server := NewServer(logger, "order-svc-test", verifier, noop.NewTracerProvider().Tracer("order-svc-test"))

	listener := bufconn.Listen(1024 * 1024)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	conn, err := grpcpkg.NewClient(
		"passthrough:///order-svc-test",
		grpcpkg.WithContextDialer(dialer),
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
	})

	return &grpcHarness{
		client:   orderv1.NewOrderServiceClient(conn),
		verifier: verifier,
	}
}

type tokenVerifierStub struct {
	err error
}

func (s tokenVerifierStub) Verify(_ string) (transport.Claims, error) {
	if s.err != nil {
		return transport.Claims{}, s.err
	}

	return transport.Claims{}, nil
}

type testTokenVerifier struct {
	calls  int
	called bool
	err    error
}

func (s *testTokenVerifier) Verify(_ string) (transport.Claims, error) {
	s.calls++
	s.called = true
	if s.err != nil {
		return transport.Claims{}, s.err
	}

	return transport.Claims{}, nil
}
