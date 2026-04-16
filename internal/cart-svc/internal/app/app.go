package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	redislib "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	grpcpkg "google.golang.org/grpc"

	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/catalog/grpc"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/repos"
	reposqlc "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/postgres/sqlc"
	adapterredis "github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/adapters/outbound/redis"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/config"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/ports/outbound"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
)

type Application struct {
	Config         *config.Config
	Database       *sql.DB
	Redis          *redislib.Client
	CatalogConn    *grpcpkg.ClientConn
	CartService    *cart.CartService
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

	if app.CartService == nil {
		app.CartService = buildCartService(app.Config, app.Database, app.Redis, app.CatalogConn)
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

func WithCatalogConn(conn *grpcpkg.ClientConn) option {
	return func(a *Application) {
		a.CatalogConn = conn
	}
}

func WithCartService(service *cart.CartService) option {
	return func(a *Application) {
		a.CartService = service
	}
}

func buildCartService(cfg *config.Config, db *sql.DB, redisClient *redislib.Client, catalogConn *grpcpkg.ClientConn) *cart.CartService {
	if db == nil {
		return nil
	}

	cartRepo := &cartRepositoryAdapter{repo: repos.NewCartRepository(db)}
	itemRepo := &cartItemRepositoryAdapter{repo: repos.NewCartItemRepository(db)}
	snapshotRepo := &productSnapshotRepositoryAdapter{repo: repos.NewProductSnapshotRepository(db)}

	var (
		catalogReader outbound.CatalogReader
		cache         outbound.CartCache
		cacheTTL      time.Duration
	)

	if cfg != nil {
		cacheTTL = cfg.Cache.ActiveCartTTL
	}

	if redisClient != nil {
		cache = adapterredis.NewCartCache(redisClient)
	}

	if catalogConn == nil {
		return cart.NewCartServiceWithCatalogAndCache(cartRepo, itemRepo, snapshotRepo, catalogReader, cache, cacheTTL)
	}

	catalogClient := catalogv1.NewCatalogServiceClient(catalogConn)
	catalogReader = adaptergrpc.NewCatalogReader(catalogClient)

	return cart.NewCartServiceWithCatalogAndCache(cartRepo, itemRepo, snapshotRepo, catalogReader, cache, cacheTTL)
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

		return domain.Cart{}, fmt.Errorf("get active cart by user id: %w", err)
	}

	cartStatus := domain.CartStatus(result.Status)
	converted, err := domain.NewCart(result.CartID, result.UserID, cartStatus, result.Currency, nil, result.CreatedAt, result.UpdatedAt)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("convert active cart: %w", err)
	}

	return converted, nil
}

func (a *cartRepositoryAdapter) CreateActive(ctx context.Context, userID uuid.UUID, currency string) (domain.Cart, error) {
	result, err := a.repo.CreateActive(ctx, userID, currency)
	if err != nil {
		if errors.Is(err, repos.ErrCartAlreadyExists) {
			return domain.Cart{}, outbound.ErrCartAlreadyExists
		}

		return domain.Cart{}, fmt.Errorf("create active cart: %w", err)
	}

	cartStatus := domain.CartStatus(result.Status)
	converted, err := domain.NewCart(result.CartID, result.UserID, cartStatus, result.Currency, nil, result.CreatedAt, result.UpdatedAt)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("convert created cart: %w", err)
	}

	return converted, nil
}

type cartItemRepositoryAdapter struct {
	repo *repos.CartItemRepository
}

func (a *cartItemRepositoryAdapter) ListByCartID(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error) {
	items, err := a.repo.ListByCartID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("list cart items by cart id: %w", err)
	}

	converted := make([]domain.CartItem, 0, len(items))
	for i := range items {
		cartItem, convErr := toDomainCartItem(items[i])
		if convErr != nil {
			return nil, fmt.Errorf("convert cart item: %w", convErr)
		}

		converted = append(converted, cartItem)
	}

	return converted, nil
}

func (a *cartItemRepositoryAdapter) Insert(ctx context.Context, params outbound.CartItemInsertParams) (domain.CartItem, error) {
	if params.Quantity > math.MaxInt32 {
		return domain.CartItem{}, fmt.Errorf("insert cart item: quantity exceeds int32")
	}

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

		return domain.CartItem{}, fmt.Errorf("insert cart item: %w", err)
	}

	converted, convErr := toDomainCartItem(item)
	if convErr != nil {
		return domain.CartItem{}, fmt.Errorf("convert inserted cart item: %w", convErr)
	}

	return converted, nil
}

func (a *cartItemRepositoryAdapter) UpdateQuantity(ctx context.Context, cartID uuid.UUID, sku string, quantity int64) (domain.CartItem, error) {
	if quantity > math.MaxInt32 {
		return domain.CartItem{}, fmt.Errorf("update cart item quantity: quantity exceeds int32")
	}

	item, err := a.repo.UpdateQuantity(ctx, cartID, sku, int32(quantity))
	if err != nil {
		if errors.Is(err, repos.ErrCartItemNotFound) {
			return domain.CartItem{}, outbound.ErrCartItemNotFound
		}

		return domain.CartItem{}, fmt.Errorf("update cart item quantity: %w", err)
	}

	converted, convErr := toDomainCartItem(item)
	if convErr != nil {
		return domain.CartItem{}, fmt.Errorf("convert updated cart item: %w", convErr)
	}

	return converted, nil
}

func (a *cartItemRepositoryAdapter) Delete(ctx context.Context, cartID uuid.UUID, sku string) error {
	err := a.repo.Delete(ctx, cartID, sku)
	if err != nil {
		if errors.Is(err, repos.ErrCartItemNotFound) {
			return outbound.ErrCartItemNotFound
		}

		return fmt.Errorf("delete cart item: %w", err)
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

		return domain.ProductSnapshot{}, fmt.Errorf("get product snapshot by sku: %w", err)
	}

	converted, convErr := toDomainProductSnapshot(snapshot)
	if convErr != nil {
		return domain.ProductSnapshot{}, fmt.Errorf("convert product snapshot: %w", convErr)
	}

	return converted, nil
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

		return domain.ProductSnapshot{}, fmt.Errorf("upsert product snapshot: %w", err)
	}

	converted, convErr := toDomainProductSnapshot(upserted)
	if convErr != nil {
		return domain.ProductSnapshot{}, fmt.Errorf("convert upserted product snapshot: %w", convErr)
	}

	return converted, nil
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

	if a.CatalogConn != nil {
		defer a.CatalogConn.Close()
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
