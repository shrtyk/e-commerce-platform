package testhelper

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	redislib "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/inbound/http"
	adaptercataloggrpc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/catalog/grpc"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/repos"
	reposqlc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
	adapterredis "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/redis"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	commonjwt "github.com/shrtyk/e-commerce-platform/internal/common/auth/jwt"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
)

const (
	serviceName = "cart-svc-test"

	TestAccessTokenKey    = "test-cart-access-token-signing-key-32b"
	TestAccessTokenIssuer = "test-identity-svc"
	TestAccessTokenTTL    = 15 * time.Minute
)

type TestStack struct {
	DB          *sql.DB
	RedisClient *redislib.Client

	CartService *cart.CartService
	HTTPHandler http.Handler
	GRPCServer  *grpcpkg.Server
	GRPCConn    *grpcpkg.ClientConn

	CatalogServer *FakeCatalogServer
	CatalogConn   *grpcpkg.ClientConn
}

func NewTestStack(t *testing.T, db *sql.DB, redisClient *redislib.Client) *TestStack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	catalogServer, catalogConn := newCatalogTestServer(t)
	catalogReader := adaptercataloggrpc.NewCatalogReader(catalogv1.NewCatalogServiceClient(catalogConn))

	cartService := cart.NewCartServiceWithCatalogAndCache(
		&cartRepositoryAdapter{repo: repos.NewCartRepository(db)},
		&cartItemRepositoryAdapter{repo: repos.NewCartItemRepository(db)},
		&productSnapshotRepositoryAdapter{repo: repos.NewProductSnapshotRepository(db)},
		catalogReader,
		adapterredis.NewCartCache(redisClient),
		5*time.Minute,
	)

	tokenVerifier := commonjwt.NewTokenVerifier(TestAccessTokenKey, TestAccessTokenIssuer)
	tracer := noop.NewTracerProvider().Tracer(serviceName)
	httpHandler := adapterhttp.NewRouter(logger, serviceName, cartService, tokenVerifier, tracer)
	grpcServer := adaptergrpc.NewServer(logger, serviceName, cartService, tokenVerifier, tracer)

	grpcConn, stopGRPC := commonintegration.StartBufconnGRPCServer(t, "cart-test", grpcServer)
	t.Cleanup(stopGRPC)

	return &TestStack{
		DB:            db,
		RedisClient:   redisClient,
		CartService:   cartService,
		HTTPHandler:   httpHandler,
		GRPCServer:    grpcServer,
		GRPCConn:      grpcConn,
		CatalogServer: catalogServer,
		CatalogConn:   catalogConn,
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

type FakeCatalogServer struct {
	catalogv1.UnimplementedCatalogServiceServer

	mu       sync.Mutex
	products map[string]CatalogProduct
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
	return &FakeCatalogServer{products: make(map[string]CatalogProduct)}
}

func (s *FakeCatalogServer) UpsertProduct(product CatalogProduct) {
	key := normalizeSKU(product.SKU)
	if key == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.products[key] = product
}

func (s *FakeCatalogServer) GetProductBySKU(_ context.Context, req *catalogv1.GetProductBySKURequest) (*catalogv1.GetProductBySKUResponse, error) {
	if req == nil {
		return nil, status.Error(codes.NotFound, "product not found")
	}

	key := normalizeSKU(req.GetSku())

	s.mu.Lock()
	product, ok := s.products[key]
	s.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "product not found")
	}

	return &catalogv1.GetProductBySKUResponse{
		Product: &catalogv1.Product{
			ProductId: product.ProductID,
			Sku:       product.SKU,
			Name:      product.Name,
			Status:    product.Status,
			Price: &commonv1.Money{
				Amount:   product.Price,
				Currency: product.Currency,
			},
		},
	}, nil
}

func (s *FakeCatalogServer) GetProduct(_ context.Context, _ *catalogv1.GetProductRequest) (*catalogv1.GetProductResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

func (s *FakeCatalogServer) ListPublishedProducts(_ context.Context, _ *catalogv1.ListPublishedProductsRequest) (*catalogv1.ListPublishedProductsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

func (s *FakeCatalogServer) ReserveStock(_ context.Context, _ *catalogv1.ReserveStockRequest) (*catalogv1.ReserveStockResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

func (s *FakeCatalogServer) ReleaseStock(_ context.Context, _ *catalogv1.ReleaseStockRequest) (*catalogv1.ReleaseStockResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake catalog")
}

type cartRepositoryAdapter struct {
	repo *repos.CartRepository
}

func (a *cartRepositoryAdapter) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (domain.Cart, error) {
	result, err := a.repo.GetActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repos.ErrCartNotFound) {
			return domain.Cart{}, outbound.ErrCartNotFound
		}

		return domain.Cart{}, err
	}

	cartStatus := domain.CartStatus(result.Status)
	converted, err := domain.NewCart(result.CartID, result.UserID, cartStatus, result.Currency, nil, result.CreatedAt, result.UpdatedAt)
	if err != nil {
		return domain.Cart{}, err
	}

	return converted, nil
}

func (a *cartRepositoryAdapter) CreateActive(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error) {
	result, err := a.repo.CreateActive(ctx, userID, currency)
	if err != nil {
		if errors.Is(err, repos.ErrCartAlreadyExists) {
			return domain.Cart{}, outbound.ErrCartAlreadyExists
		}

		return domain.Cart{}, err
	}

	cartStatus := domain.CartStatus(result.Status)
	converted, err := domain.NewCart(result.CartID, result.UserID, cartStatus, result.Currency, nil, result.CreatedAt, result.UpdatedAt)
	if err != nil {
		return domain.Cart{}, err
	}

	return converted, nil
}

type cartItemRepositoryAdapter struct {
	repo *repos.CartItemRepository
}

func (a *cartItemRepositoryAdapter) ListByCartID(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error) {
	items, err := a.repo.ListByCartID(ctx, cartID)
	if err != nil {
		return nil, err
	}

	converted := make([]domain.CartItem, 0, len(items))
	for i := range items {
		cartItem, convErr := toDomainCartItem(items[i])
		if convErr != nil {
			return nil, convErr
		}

		converted = append(converted, cartItem)
	}

	return converted, nil
}

func (a *cartItemRepositoryAdapter) Insert(ctx context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error) {
	item, err := a.repo.Insert(ctx, reposqlc.InsertCartItemParams{
		CartID:      params.CartID,
		Sku:         params.SKU,
		Quantity:    int32(params.Quantity),
		UnitPrice:   params.UnitPrice,
		Currency:    params.Currency,
		ProductName: params.ProductName,
	})
	if err != nil {
		if errors.Is(err, repos.ErrCartNotFound) {
			return domain.CartItem{}, outbound.ErrCartNotFound
		}
		if errors.Is(err, repos.ErrCartItemAlreadyExists) {
			return domain.CartItem{}, outbound.ErrCartItemAlreadyExists
		}
		if errors.Is(err, repos.ErrProductSnapshotNotFound) {
			return domain.CartItem{}, outbound.ErrProductSnapshotNotFound
		}

		return domain.CartItem{}, err
	}

	return toDomainCartItem(item)
}

func (a *cartItemRepositoryAdapter) UpdateQuantity(ctx context.Context, cartID uuid.UUID, sku string, quantity int64) (domain.CartItem, error) {
	item, err := a.repo.UpdateQuantity(ctx, cartID, sku, int32(quantity))
	if err != nil {
		if errors.Is(err, repos.ErrCartItemNotFound) {
			return domain.CartItem{}, outbound.ErrCartItemNotFound
		}

		return domain.CartItem{}, err
	}

	return toDomainCartItem(item)
}

func (a *cartItemRepositoryAdapter) Delete(ctx context.Context, cartID uuid.UUID, sku string) error {
	err := a.repo.Delete(ctx, cartID, sku)
	if err != nil {
		if errors.Is(err, repos.ErrCartItemNotFound) {
			return outbound.ErrCartItemNotFound
		}

		return err
	}

	return nil
}

type productSnapshotRepositoryAdapter struct {
	repo *repos.ProductSnapshotRepository
}

func (a *productSnapshotRepositoryAdapter) GetBySKU(ctx context.Context, sku string) (domain.ProductSnapshot, error) {
	snapshot, err := a.repo.GetBySKU(ctx, sku)
	if err != nil {
		if errors.Is(err, repos.ErrProductSnapshotNotFound) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotNotFound
		}

		return domain.ProductSnapshot{}, err
	}

	return toDomainProductSnapshot(snapshot)
}

func (a *productSnapshotRepositoryAdapter) Upsert(ctx context.Context, snapshot domain.ProductSnapshot) (domain.ProductSnapshot, error) {
	upserted, err := a.repo.Upsert(ctx, reposqlc.UpsertProductSnapshotParams{
		Sku:       snapshot.SKU,
		ProductID: toNullUUID(snapshot.ProductID),
		Name:      snapshot.Name,
		UnitPrice: snapshot.UnitPrice,
		Currency:  snapshot.Currency,
	})
	if err != nil {
		if errors.Is(err, repos.ErrProductSnapshotConflict) {
			return domain.ProductSnapshot{}, outbound.ErrProductSnapshotConflict
		}

		return domain.ProductSnapshot{}, err
	}

	return toDomainProductSnapshot(upserted)
}

func toDomainCartItem(item reposqlc.CartItem) (domain.CartItem, error) {
	return domain.NewCartItem(
		item.Sku,
		item.ProductName,
		int64(item.Quantity),
		item.UnitPrice,
		item.Currency,
		item.CreatedAt,
		item.UpdatedAt,
	)
}

func toDomainProductSnapshot(snapshot reposqlc.ProductSnapshot) (domain.ProductSnapshot, error) {
	productID := fromNullUUID(snapshot.ProductID)

	return domain.NewProductSnapshot(
		snapshot.Sku,
		productID,
		snapshot.Name,
		snapshot.UnitPrice,
		snapshot.Currency,
		snapshot.CreatedAt,
		snapshot.UpdatedAt,
	)
}

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}

	return uuid.NullUUID{UUID: *id, Valid: true}
}

func fromNullUUID(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}

	value := id.UUID

	return &value
}

func newCatalogTestServer(t *testing.T) (*FakeCatalogServer, *grpcpkg.ClientConn) {
	t.Helper()

	server := grpcpkg.NewServer()
	fakeCatalog := NewFakeCatalogServer()
	catalogv1.RegisterCatalogServiceServer(server, fakeCatalog)

	conn, stop := commonintegration.StartBufconnGRPCServer(t, "catalog-test", server)
	t.Cleanup(stop)

	return fakeCatalog, conn
}

func normalizeSKU(value string) string {
	return strings.TrimSpace(strings.ToUpper(value))
}
