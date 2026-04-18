package testhelper

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http"
	adaptercheckoutgrpc "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/checkout/grpc"
	adapterevents "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/events"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/outbox"
	adapterpostgresrepos "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/service/checkout"
)

const (
	serviceName            = "order-svc-test"
	TestAccessTokenKey     = "test-order-access-token-signing-key-32"
	TestAccessTokenIssuer  = "test-identity-svc"
	TestAccessTokenTTL     = 15 * time.Minute
	defaultPaymentProvider = "card"
)

type TestStack struct {
	DB *sql.DB

	CheckoutService *checkout.Service
	HTTPHandler     http.Handler
	GRPCServer      *grpcpkg.Server
	GRPCConn        *grpcpkg.ClientConn

	CartServer    *FakeCartServer
	CartConn      *grpcpkg.ClientConn
	CatalogServer *FakeCatalogServer
	CatalogConn   *grpcpkg.ClientConn
	PaymentServer *FakePaymentServer
	PaymentConn   *grpcpkg.ClientConn
}

func NewTestStack(t *testing.T, db *sql.DB) *TestStack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracer := noop.NewTracerProvider().Tracer(serviceName)

	cartServer, cartConn := newCartTestServer(t)
	catalogServer, catalogConn := newCatalogTestServer(t)
	paymentServer, paymentConn := newPaymentTestServer(t)

	orderRepository := adapterpostgresrepos.NewOrderRepository(db)
	sagaRepository := adapterpostgresrepos.NewOrderSagaStateRepository(db)
	outboxRepository := adapteroutbox.NewRepository(db)
	publisher := adapterevents.MustCreateOutboxEventPublisher(outboxRepository)

	txProvider := sqltx.NewProvider(db, func(sqlTx *sql.Tx) checkout.TransactionRepos {
		return checkout.TransactionRepos{
			Orders:    adapterpostgresrepos.NewOrderRepositoryFromTx(sqlTx),
			Saga:      adapterpostgresrepos.NewOrderSagaStateRepositoryFromTx(sqlTx),
			Publisher: adapterevents.MustCreateOutboxEventPublisher(adapteroutbox.NewRepositoryFromTx(sqlTx)),
		}
	})

	checkoutService := checkout.NewService(
		orderRepository,
		sagaRepository,
		adaptercheckoutgrpc.NewCheckoutSnapshotRepository(
			cartv1.NewCartServiceClient(cartConn),
			catalogv1.NewCatalogServiceClient(catalogConn),
		),
		adaptercheckoutgrpc.NewStockReservationService(catalogv1.NewCatalogServiceClient(catalogConn)),
		adaptercheckoutgrpc.NewStockReleaseService(catalogv1.NewCatalogServiceClient(catalogConn)),
		adaptercheckoutgrpc.NewCheckoutPaymentService(paymentv1.NewPaymentServiceClient(paymentConn)),
		checkout.NewCheckoutIdempotencyGuard(orderRepository),
	).WithEventing(publisher, txProvider, serviceName)

	tokenVerifier := commonjwt.NewTokenVerifier(TestAccessTokenKey, TestAccessTokenIssuer)
	httpHandler := adapterhttp.NewRouter(logger, serviceName, db, checkoutService, tokenVerifier, tracer)
	grpcServer := adaptergrpc.NewServer(logger, serviceName, checkoutService, tokenVerifier, tracer)

	grpcConn, stopGRPC := commonintegration.StartBufconnGRPCServer(t, "order-test", grpcServer)
	t.Cleanup(stopGRPC)

	return &TestStack{
		DB:              db,
		CheckoutService: checkoutService,
		HTTPHandler:     httpHandler,
		GRPCServer:      grpcServer,
		GRPCConn:        grpcConn,
		CartServer:      cartServer,
		CartConn:        cartConn,
		CatalogServer:   catalogServer,
		CatalogConn:     catalogConn,
		PaymentServer:   paymentServer,
		PaymentConn:     paymentConn,
	}
}

func MintAccessToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	require.NotEqual(t, uuid.Nil, userID)

	now := time.Now().UTC()
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, accessTokenClaims{
		Role:   "user",
		Status: "active",
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    TestAccessTokenIssuer,
			Subject:   userID.String(),
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(TestAccessTokenTTL)),
		},
	})

	tokenString, err := token.SignedString([]byte(TestAccessTokenKey))
	require.NoError(t, err)

	return tokenString
}

type accessTokenClaims struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	jwtv5.RegisteredClaims
}

type FakeCartServer struct {
	cartv1.UnimplementedCartServiceServer

	mu        sync.Mutex
	snapshots map[string]*cartv1.CheckoutSnapshot
}

func NewFakeCartServer() *FakeCartServer {
	return &FakeCartServer{snapshots: make(map[string]*cartv1.CheckoutSnapshot)}
}

func (s *FakeCartServer) SetSnapshot(snapshot *cartv1.CheckoutSnapshot) {
	if snapshot == nil || snapshot.GetUserId() == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.GetUserId()] = snapshot
}

func (s *FakeCartServer) GetCheckoutSnapshot(_ context.Context, req *cartv1.GetCheckoutSnapshotRequest) (*cartv1.GetCheckoutSnapshotResponse, error) {
	if req == nil || req.GetUserId() == "" {
		return nil, status.Error(codes.NotFound, "checkout snapshot not found")
	}

	s.mu.Lock()
	snapshot, ok := s.snapshots[req.GetUserId()]
	s.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "checkout snapshot not found")
	}

	return &cartv1.GetCheckoutSnapshotResponse{Snapshot: snapshot}, nil
}

func (s *FakeCartServer) GetActiveCart(context.Context, *cartv1.GetActiveCartRequest) (*cartv1.GetActiveCartResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake cart")
}

func (s *FakeCartServer) AddCartItem(context.Context, *cartv1.AddCartItemRequest) (*cartv1.AddCartItemResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake cart")
}

func (s *FakeCartServer) UpdateCartItem(context.Context, *cartv1.UpdateCartItemRequest) (*cartv1.UpdateCartItemResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake cart")
}

func (s *FakeCartServer) RemoveCartItem(context.Context, *cartv1.RemoveCartItemRequest) (*cartv1.RemoveCartItemResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake cart")
}

type FakeCatalogServer struct {
	catalogv1.UnimplementedCatalogServiceServer

	mu            sync.Mutex
	productsBySKU map[string]CatalogProduct
	reserveErr    error
	releaseErr    error
	releaseCalls  []catalogv1.ReleaseStockRequest
	reserveCalls  []catalogv1.ReserveStockRequest
}

type CatalogProduct struct {
	ProductID string
	SKU       string
	Name      string
	Price     int64
	Currency  string
	Status    catalogv1.ProductStatus
}

func NewFakeCatalogServer() *FakeCatalogServer {
	return &FakeCatalogServer{productsBySKU: make(map[string]CatalogProduct)}
}

func (s *FakeCatalogServer) UpsertProduct(product CatalogProduct) {
	if product.SKU == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.productsBySKU[product.SKU] = product
}

func (s *FakeCatalogServer) SetReserveError(err error) {
	s.mu.Lock()
	s.reserveErr = err
	s.mu.Unlock()
}

func (s *FakeCatalogServer) SetReleaseError(err error) {
	s.mu.Lock()
	s.releaseErr = err
	s.mu.Unlock()
}

func (s *FakeCatalogServer) ReleaseCalls() []catalogv1.ReleaseStockRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]catalogv1.ReleaseStockRequest, len(s.releaseCalls))
	copy(result, s.releaseCalls)
	return result
}

func (s *FakeCatalogServer) GetProductBySKU(_ context.Context, req *catalogv1.GetProductBySKURequest) (*catalogv1.GetProductBySKUResponse, error) {
	if req == nil || req.GetSku() == "" {
		return nil, status.Error(codes.NotFound, "product not found")
	}

	s.mu.Lock()
	product, ok := s.productsBySKU[req.GetSku()]
	s.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "product not found")
	}

	return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{
		ProductId: product.ProductID,
		Sku:       product.SKU,
		Name:      product.Name,
		Status:    product.Status,
		Price:     &commonv1.Money{Amount: product.Price, Currency: product.Currency},
	}}, nil
}

func (s *FakeCatalogServer) ReserveStock(_ context.Context, req *catalogv1.ReserveStockRequest) (*catalogv1.ReserveStockResponse, error) {
	s.mu.Lock()
	if req != nil {
		s.reserveCalls = append(s.reserveCalls, *req)
	}
	err := s.reserveErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	return &catalogv1.ReserveStockResponse{Accepted: true}, nil
}

func (s *FakeCatalogServer) ReleaseStock(_ context.Context, req *catalogv1.ReleaseStockRequest) (*catalogv1.ReleaseStockResponse, error) {
	s.mu.Lock()
	if req != nil {
		s.releaseCalls = append(s.releaseCalls, *req)
	}
	err := s.releaseErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	return &catalogv1.ReleaseStockResponse{Accepted: true}, nil
}

func (s *FakeCatalogServer) GetProduct(context.Context, *catalogv1.GetProductRequest) (*catalogv1.GetProductResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

func (s *FakeCatalogServer) ListPublishedProducts(context.Context, *catalogv1.ListPublishedProductsRequest) (*catalogv1.ListPublishedProductsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

type FakePaymentServer struct {
	paymentv1.UnimplementedPaymentServiceServer

	mu         sync.Mutex
	nextResult *paymentv1.PaymentAttempt
	nextErr    error
	requests   []paymentv1.InitiatePaymentRequest
}

func NewFakePaymentServer() *FakePaymentServer {
	return &FakePaymentServer{}
}

func (s *FakePaymentServer) SetResult(result *paymentv1.PaymentAttempt, err error) {
	s.mu.Lock()
	s.nextResult = result
	s.nextErr = err
	s.mu.Unlock()
}

func (s *FakePaymentServer) Requests() []paymentv1.InitiatePaymentRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]paymentv1.InitiatePaymentRequest, len(s.requests))
	copy(result, s.requests)
	return result
}

func (s *FakePaymentServer) InitiatePayment(_ context.Context, req *paymentv1.InitiatePaymentRequest) (*paymentv1.InitiatePaymentResponse, error) {
	s.mu.Lock()
	if req != nil {
		s.requests = append(s.requests, *req)
	}
	result := s.nextResult
	err := s.nextErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}

	if result == nil {
		result = &paymentv1.PaymentAttempt{
			PaymentAttemptId: uuid.NewString(),
			OrderId:          req.GetOrderId(),
			Status:           paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED,
			ProviderName:     defaultPaymentProvider,
			Amount:           req.GetAmount(),
		}
	}

	return &paymentv1.InitiatePaymentResponse{PaymentAttempt: result}, nil
}

func HTTPJSONRequest(t *testing.T, method string, path string, token string, body any) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req
}

func newCartTestServer(t *testing.T) (*FakeCartServer, *grpcpkg.ClientConn) {
	t.Helper()
	server := grpcpkg.NewServer()
	fake := NewFakeCartServer()
	cartv1.RegisterCartServiceServer(server, fake)
	conn, stop := commonintegration.StartBufconnGRPCServer(t, "order-cart-test", server)
	t.Cleanup(stop)
	return fake, conn
}

func newCatalogTestServer(t *testing.T) (*FakeCatalogServer, *grpcpkg.ClientConn) {
	t.Helper()
	server := grpcpkg.NewServer()
	fake := NewFakeCatalogServer()
	catalogv1.RegisterCatalogServiceServer(server, fake)
	conn, stop := commonintegration.StartBufconnGRPCServer(t, "order-catalog-test", server)
	t.Cleanup(stop)
	return fake, conn
}

func newPaymentTestServer(t *testing.T) (*FakePaymentServer, *grpcpkg.ClientConn) {
	t.Helper()
	server := grpcpkg.NewServer()
	fake := NewFakePaymentServer()
	paymentv1.RegisterPaymentServiceServer(server, fake)
	conn, stop := commonintegration.StartBufconnGRPCServer(t, "order-payment-test", server)
	t.Cleanup(stop)
	return fake, conn
}
