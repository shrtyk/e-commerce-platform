package grpc

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

func TestCheckoutPaymentServiceInitiatePaymentStatusHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status paymentv1.PaymentStatus
		want   error
	}{
		{name: "succeeded is success", status: paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED},
		{name: "failed maps declined", status: paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED, want: outbound.ErrPaymentDeclined},
		{name: "processing maps conflict", status: paymentv1.PaymentStatus_PAYMENT_STATUS_PROCESSING, want: outbound.ErrPaymentConflict},
		{name: "initiated maps conflict", status: paymentv1.PaymentStatus_PAYMENT_STATUS_INITIATED, want: outbound.ErrPaymentConflict},
		{name: "unspecified maps conflict", status: paymentv1.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED, want: outbound.ErrPaymentConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewCheckoutPaymentService(fakePaymentClient{
				initiatePaymentFunc: func(context.Context, *paymentv1.InitiatePaymentRequest, ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
					return &paymentv1.InitiatePaymentResponse{PaymentAttempt: &paymentv1.PaymentAttempt{Status: tt.status}}, nil
				},
			})

			err := svc.InitiatePayment(context.Background(), outbound.InitiatePaymentInput{
				OrderID:         uuid.New(),
				Amount:          1000,
				Currency:        "USD",
				IdempotencyKey:  "idem",
				PaymentProvider: "default",
			})

			if tt.want == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestCheckoutPaymentServiceInitiatePaymentGRPCErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		grpcErr error
		want    error
	}{
		{name: "failed precondition maps declined", grpcErr: status.Error(codes.FailedPrecondition, "declined"), want: outbound.ErrPaymentDeclined},
		{name: "aborted maps conflict", grpcErr: status.Error(codes.Aborted, "conflict"), want: outbound.ErrPaymentConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewCheckoutPaymentService(fakePaymentClient{
				initiatePaymentFunc: func(context.Context, *paymentv1.InitiatePaymentRequest, ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
					return nil, tt.grpcErr
				},
			})

			err := svc.InitiatePayment(context.Background(), outbound.InitiatePaymentInput{OrderID: uuid.New()})
			require.Error(t, err)
			require.ErrorIs(t, err, tt.want)
		})
	}

	t.Run("unknown grpc error wraps", func(t *testing.T) {
		t.Parallel()

		rpcErr := status.Error(codes.Internal, "boom")
		svc := NewCheckoutPaymentService(fakePaymentClient{
			initiatePaymentFunc: func(context.Context, *paymentv1.InitiatePaymentRequest, ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
				return nil, rpcErr
			},
		})

		err := svc.InitiatePayment(context.Background(), outbound.InitiatePaymentInput{OrderID: uuid.New()})
		require.Error(t, err)
		require.ErrorContains(t, err, "payment initiate payment")
		require.ErrorIs(t, err, rpcErr)
	})
}

func TestCheckoutPaymentServiceInitiatePaymentRejectsEmptyAttempt(t *testing.T) {
	t.Parallel()

	svc := NewCheckoutPaymentService(fakePaymentClient{
		initiatePaymentFunc: func(context.Context, *paymentv1.InitiatePaymentRequest, ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
			return &paymentv1.InitiatePaymentResponse{}, nil
		},
	})

	err := svc.InitiatePayment(context.Background(), outbound.InitiatePaymentInput{OrderID: uuid.New()})
	require.Error(t, err)
	require.ErrorContains(t, err, "empty payment attempt")
}

func TestCheckoutPaymentServiceInitiatePaymentMapsRequestFields(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()

	svc := NewCheckoutPaymentService(fakePaymentClient{
		initiatePaymentFunc: func(_ context.Context, req *paymentv1.InitiatePaymentRequest, _ ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
			require.Equal(t, orderID.String(), req.GetOrderId())
			require.Equal(t, int64(4200), req.GetAmount().GetAmount())
			require.Equal(t, "USD", req.GetAmount().GetCurrency())
			require.Equal(t, "provider-x", req.GetProviderName())
			require.Equal(t, "idem-4200", req.GetIdempotencyKey())

			return &paymentv1.InitiatePaymentResponse{PaymentAttempt: &paymentv1.PaymentAttempt{
				Status: paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED,
				Amount: &commonv1.Money{Amount: 4200, Currency: "USD"},
			}}, nil
		},
	})

	err := svc.InitiatePayment(context.Background(), outbound.InitiatePaymentInput{
		OrderID:         orderID,
		Amount:          4200,
		Currency:        "USD",
		PaymentProvider: "provider-x",
		IdempotencyKey:  "idem-4200",
	})
	require.NoError(t, err)
}

func TestStockReservationServiceReserveStockGRPCErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "not found maps sku not found", err: status.Error(codes.NotFound, "missing"), wantErr: outbound.ErrStockReservationSKUNotFound},
		{name: "failed precondition maps unavailable", err: status.Error(codes.FailedPrecondition, "unavailable"), wantErr: outbound.ErrStockReservationUnavailable},
		{name: "aborted maps conflict", err: status.Error(codes.Aborted, "conflict"), wantErr: outbound.ErrStockReservationConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewStockReservationService(fakeCatalogClient{
				reserveStockFunc: func(context.Context, *catalogv1.ReserveStockRequest, ...grpc.CallOption) (*catalogv1.ReserveStockResponse, error) {
					return nil, tt.err
				},
			})

			err := svc.ReserveStock(context.Background(), outbound.ReserveStockInput{OrderID: uuid.New()})
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("unknown grpc error wraps", func(t *testing.T) {
		t.Parallel()

		rpcErr := status.Error(codes.Internal, "boom")
		svc := NewStockReservationService(fakeCatalogClient{
			reserveStockFunc: func(context.Context, *catalogv1.ReserveStockRequest, ...grpc.CallOption) (*catalogv1.ReserveStockResponse, error) {
				return nil, rpcErr
			},
		})

		err := svc.ReserveStock(context.Background(), outbound.ReserveStockInput{OrderID: uuid.New()})
		require.Error(t, err)
		require.ErrorContains(t, err, "catalog reserve stock")
		require.ErrorIs(t, err, rpcErr)
	})
}

func TestStockReleaseServiceReleaseStockGRPCErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "not found maps release not found", err: status.Error(codes.NotFound, "missing"), wantErr: outbound.ErrStockReleaseNotFound},
		{name: "failed precondition maps unavailable", err: status.Error(codes.FailedPrecondition, "unavailable"), wantErr: outbound.ErrStockReleaseUnavailable},
		{name: "aborted maps conflict", err: status.Error(codes.Aborted, "conflict"), wantErr: outbound.ErrStockReleaseConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewStockReleaseService(fakeCatalogClient{
				releaseStockFunc: func(context.Context, *catalogv1.ReleaseStockRequest, ...grpc.CallOption) (*catalogv1.ReleaseStockResponse, error) {
					return nil, tt.err
				},
			})

			err := svc.ReleaseStock(context.Background(), outbound.ReleaseStockInput{OrderID: uuid.New()})
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("unknown grpc error wraps", func(t *testing.T) {
		t.Parallel()

		rpcErr := status.Error(codes.Internal, "boom")
		svc := NewStockReleaseService(fakeCatalogClient{
			releaseStockFunc: func(context.Context, *catalogv1.ReleaseStockRequest, ...grpc.CallOption) (*catalogv1.ReleaseStockResponse, error) {
				return nil, rpcErr
			},
		})

		err := svc.ReleaseStock(context.Background(), outbound.ReleaseStockInput{OrderID: uuid.New()})
		require.Error(t, err)
		require.ErrorContains(t, err, "catalog release stock")
		require.ErrorIs(t, err, rpcErr)
	})
}

func TestCheckoutSnapshotRepositoryGetCheckoutSnapshotMappings(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	t.Run("cart not found maps checkout snapshot not found", func(t *testing.T) {
		t.Parallel()

		repo := NewCheckoutSnapshotRepository(
			fakeCartClient{
				getCheckoutSnapshotFunc: func(context.Context, *cartv1.GetCheckoutSnapshotRequest, ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error) {
					return nil, status.Error(codes.NotFound, "missing")
				},
			},
			fakeCatalogClient{},
		)

		_, err := repo.GetCheckoutSnapshot(context.Background(), userID)
		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrCheckoutSnapshotNotFound)
	})

	t.Run("catalog not found maps sku not found", func(t *testing.T) {
		t.Parallel()

		repo := NewCheckoutSnapshotRepository(
			fakeCartClient{
				getCheckoutSnapshotFunc: func(context.Context, *cartv1.GetCheckoutSnapshotRequest, ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error) {
					return &cartv1.GetCheckoutSnapshotResponse{Snapshot: &cartv1.CheckoutSnapshot{
						TotalAmount: &commonv1.Money{Amount: 1000, Currency: "USD"},
						Items: []*cartv1.CartItem{{
							Sku:       "SKU-404",
							Name:      "Missing",
							Quantity:  1,
							UnitPrice: &commonv1.Money{Amount: 1000, Currency: "USD"},
							LineTotal: &commonv1.Money{Amount: 1000, Currency: "USD"},
						}},
					}}, nil
				},
			},
			fakeCatalogClient{
				getProductBySKUFunc: func(context.Context, *catalogv1.GetProductBySKURequest, ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return nil, status.Error(codes.NotFound, "missing product")
				},
			},
		)

		_, err := repo.GetCheckoutSnapshot(context.Background(), userID)
		require.Error(t, err)
		require.ErrorIs(t, err, outbound.ErrStockReservationSKUNotFound)
	})

	t.Run("invalid product id returns validation error", func(t *testing.T) {
		t.Parallel()

		repo := NewCheckoutSnapshotRepository(
			fakeCartClient{
				getCheckoutSnapshotFunc: func(context.Context, *cartv1.GetCheckoutSnapshotRequest, ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error) {
					return &cartv1.GetCheckoutSnapshotResponse{Snapshot: &cartv1.CheckoutSnapshot{
						TotalAmount: &commonv1.Money{Amount: 1000, Currency: "USD"},
						Items: []*cartv1.CartItem{{
							Sku:       "SKU-1",
							Name:      "Product",
							Quantity:  1,
							UnitPrice: &commonv1.Money{Amount: 1000, Currency: "USD"},
							LineTotal: &commonv1.Money{Amount: 1000, Currency: "USD"},
						}},
					}}, nil
				},
			},
			fakeCatalogClient{
				getProductBySKUFunc: func(context.Context, *catalogv1.GetProductBySKURequest, ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{ProductId: "bad-id"}}, nil
				},
			},
		)

		_, err := repo.GetCheckoutSnapshot(context.Background(), userID)
		require.Error(t, err)
		require.ErrorContains(t, err, "invalid product id")
	})

	t.Run("quantity overflow returns validation error", func(t *testing.T) {
		t.Parallel()

		repo := NewCheckoutSnapshotRepository(
			fakeCartClient{
				getCheckoutSnapshotFunc: func(context.Context, *cartv1.GetCheckoutSnapshotRequest, ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error) {
					return &cartv1.GetCheckoutSnapshotResponse{Snapshot: &cartv1.CheckoutSnapshot{
						TotalAmount: &commonv1.Money{Amount: 1000, Currency: "USD"},
						Items: []*cartv1.CartItem{{
							Sku:       "SKU-overflow",
							Name:      "Product",
							Quantity:  math.MaxInt32 + 1,
							UnitPrice: &commonv1.Money{Amount: 1000, Currency: "USD"},
							LineTotal: &commonv1.Money{Amount: 1000, Currency: "USD"},
						}},
					}}, nil
				},
			},
			fakeCatalogClient{
				getProductBySKUFunc: func(context.Context, *catalogv1.GetProductBySKURequest, ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
					return &catalogv1.GetProductBySKUResponse{Product: &catalogv1.Product{ProductId: uuid.NewString()}}, nil
				},
			},
		)

		_, err := repo.GetCheckoutSnapshot(context.Background(), userID)
		require.Error(t, err)
		require.ErrorContains(t, err, "quantity out of int32 range")
	})
}

type fakePaymentClient struct {
	initiatePaymentFunc func(ctx context.Context, in *paymentv1.InitiatePaymentRequest, opts ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error)
}

func (f fakePaymentClient) InitiatePayment(ctx context.Context, in *paymentv1.InitiatePaymentRequest, opts ...grpc.CallOption) (*paymentv1.InitiatePaymentResponse, error) {
	if f.initiatePaymentFunc == nil {
		return nil, errors.New("initiate payment func is not configured")
	}

	return f.initiatePaymentFunc(ctx, in, opts...)
}

type fakeCatalogClient struct {
	getProductBySKUFunc func(ctx context.Context, in *catalogv1.GetProductBySKURequest, opts ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error)
	reserveStockFunc    func(ctx context.Context, in *catalogv1.ReserveStockRequest, opts ...grpc.CallOption) (*catalogv1.ReserveStockResponse, error)
	releaseStockFunc    func(ctx context.Context, in *catalogv1.ReleaseStockRequest, opts ...grpc.CallOption) (*catalogv1.ReleaseStockResponse, error)
}

func (f fakeCatalogClient) GetProduct(context.Context, *catalogv1.GetProductRequest, ...grpc.CallOption) (*catalogv1.GetProductResponse, error) {
	return nil, errors.New("get product not configured")
}

func (f fakeCatalogClient) GetProductBySKU(ctx context.Context, in *catalogv1.GetProductBySKURequest, opts ...grpc.CallOption) (*catalogv1.GetProductBySKUResponse, error) {
	if f.getProductBySKUFunc == nil {
		return nil, errors.New("get product by sku func is not configured")
	}

	return f.getProductBySKUFunc(ctx, in, opts...)
}

func (f fakeCatalogClient) ListPublishedProducts(context.Context, *catalogv1.ListPublishedProductsRequest, ...grpc.CallOption) (*catalogv1.ListPublishedProductsResponse, error) {
	return nil, errors.New("list published products not configured")
}

func (f fakeCatalogClient) ReserveStock(ctx context.Context, in *catalogv1.ReserveStockRequest, opts ...grpc.CallOption) (*catalogv1.ReserveStockResponse, error) {
	if f.reserveStockFunc == nil {
		return nil, errors.New("reserve stock func is not configured")
	}

	return f.reserveStockFunc(ctx, in, opts...)
}

func (f fakeCatalogClient) ReleaseStock(ctx context.Context, in *catalogv1.ReleaseStockRequest, opts ...grpc.CallOption) (*catalogv1.ReleaseStockResponse, error) {
	if f.releaseStockFunc == nil {
		return nil, errors.New("release stock func is not configured")
	}

	return f.releaseStockFunc(ctx, in, opts...)
}

type fakeCartClient struct {
	getCheckoutSnapshotFunc func(ctx context.Context, in *cartv1.GetCheckoutSnapshotRequest, opts ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error)
}

func (f fakeCartClient) GetActiveCart(context.Context, *cartv1.GetActiveCartRequest, ...grpc.CallOption) (*cartv1.GetActiveCartResponse, error) {
	return nil, errors.New("get active cart not configured")
}

func (f fakeCartClient) AddCartItem(context.Context, *cartv1.AddCartItemRequest, ...grpc.CallOption) (*cartv1.AddCartItemResponse, error) {
	return nil, errors.New("add cart item not configured")
}

func (f fakeCartClient) UpdateCartItem(context.Context, *cartv1.UpdateCartItemRequest, ...grpc.CallOption) (*cartv1.UpdateCartItemResponse, error) {
	return nil, errors.New("update cart item not configured")
}

func (f fakeCartClient) RemoveCartItem(context.Context, *cartv1.RemoveCartItemRequest, ...grpc.CallOption) (*cartv1.RemoveCartItemResponse, error) {
	return nil, errors.New("remove cart item not configured")
}

func (f fakeCartClient) GetCheckoutSnapshot(ctx context.Context, in *cartv1.GetCheckoutSnapshotRequest, opts ...grpc.CallOption) (*cartv1.GetCheckoutSnapshotResponse, error) {
	if f.getCheckoutSnapshotFunc == nil {
		return nil, errors.New("get checkout snapshot func is not configured")
	}

	return f.getCheckoutSnapshotFunc(ctx, in, opts...)
}
