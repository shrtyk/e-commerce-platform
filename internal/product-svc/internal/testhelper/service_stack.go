package testhelper

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/events"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

const (
	bufconnBufferSize = 1024 * 1024
	serviceName       = "product-svc-test"
)

type TestStack struct {
	DB             *sql.DB
	CatalogService *catalog.CatalogService
	HTTPHandler    http.Handler
	GRPCServer     *grpcpkg.Server
	GRPCConn       *grpcpkg.ClientConn
}

func NewTestStack(t *testing.T, db *sql.DB) *TestStack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	productRepository := repos.NewProductRepository(db)
	stockRepository := repos.NewStockRepository(db)
	outboxRepository := adapteroutbox.NewRepository(db)
	eventPublisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepository)

	txProvider := sqltx.NewProvider(db, func(tx *sql.Tx) catalog.CatalogRepos {
		return catalog.CatalogRepos{
			Products:  repos.NewProductRepositoryFromTx(tx),
			Stocks:    repos.NewStockRepositoryFromTx(tx),
			Publisher: adapterevents.MustCreateOutboxEventPublisher(adapteroutbox.NewRepositoryFromTx(tx)),
		}
	})

	catalogService := catalog.NewCatalogService(productRepository, stockRepository, eventPublisher, txProvider, serviceName)
	tracer := noop.NewTracerProvider().Tracer(serviceName)
	httpHandler := adapterhttp.NewRouter(logger, serviceName, catalogService, tracer)
	grpcServer := adaptergrpc.NewServer(logger, serviceName, catalogService, tracer)

	listener := bufconn.Listen(bufconnBufferSize)
	ready := make(chan struct{})
	serveDone := make(chan struct{})
	readyListener := &readySignalListener{
		Listener: listener,
		ready:    ready,
	}

	go func() {
		defer close(serveDone)
		_ = grpcServer.Serve(readyListener)
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
		t.Fatal("grpc server did not start accepting connections")
	}

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	grpcConn, err := grpcpkg.NewClient(
		"passthrough:///product-test",
		grpcpkg.WithContextDialer(dialer),
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
		t.Fatalf("create grpc client connection: %v", err)
	}

	t.Cleanup(func() {
		require.NoError(t, grpcConn.Close())
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
	})

	return &TestStack{
		DB:             db,
		CatalogService: catalogService,
		HTTPHandler:    httpHandler,
		GRPCServer:     grpcServer,
		GRPCConn:       grpcConn,
	}
}

type readySignalListener struct {
	net.Listener
	once  sync.Once
	ready chan struct{}
}

func (l *readySignalListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		close(l.ready)
	})

	return l.Listener.Accept()
}
