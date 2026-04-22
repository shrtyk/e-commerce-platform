package testhelper

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"

	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/inbound/http"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/events"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/service/catalog"
)

const (
	serviceName = "product-svc-test"
	TestAuthKey = "product-svc-test-access-key"
	TestAuthIssuer = "ecom-identity-svc"
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
	tokenVerifier := commonjwt.NewTokenVerifier(TestAuthKey, TestAuthIssuer)
	httpHandler := adapterhttp.NewRouter(logger, serviceName, catalogService, tracer, tokenVerifier)
	grpcServer := adaptergrpc.NewServer(logger, serviceName, catalogService, tracer)

	grpcConn, stopGRPC := commonintegration.StartBufconnGRPCServer(t, "product-test", grpcServer)
	t.Cleanup(stopGRPC)

	return &TestStack{
		DB:             db,
		CatalogService: catalogService,
		HTTPHandler:    httpHandler,
		GRPCServer:     grpcServer,
		GRPCConn:       grpcConn,
	}
}
