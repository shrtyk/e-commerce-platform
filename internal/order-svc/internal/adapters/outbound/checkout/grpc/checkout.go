package grpc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

type shopperAuthorizationContextKey struct{}

type CheckoutSnapshotRepository struct {
	cartClient    cartv1.CartServiceClient
	catalogClient catalogv1.CatalogServiceClient
}

func NewCheckoutSnapshotRepository(
	cartClient cartv1.CartServiceClient,
	catalogClient catalogv1.CatalogServiceClient,
) *CheckoutSnapshotRepository {
	return &CheckoutSnapshotRepository{cartClient: cartClient, catalogClient: catalogClient}
}

func (a *CheckoutSnapshotRepository) GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (outbound.CheckoutSnapshot, error) {
	response, err := a.cartClient.GetCheckoutSnapshot(withAuthorizationMetadata(ctx), &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return outbound.CheckoutSnapshot{}, outbound.ErrCheckoutSnapshotNotFound
		}

		return outbound.CheckoutSnapshot{}, fmt.Errorf("cart get checkout snapshot: %w", err)
	}

	snapshot := response.GetSnapshot()
	if snapshot == nil {
		return outbound.CheckoutSnapshot{}, errors.New("cart get checkout snapshot: empty snapshot")
	}

	total := snapshot.GetTotalAmount()
	if total == nil {
		return outbound.CheckoutSnapshot{}, errors.New("cart get checkout snapshot: missing total amount")
	}

	items := make([]outbound.CheckoutSnapshotItem, 0, len(snapshot.GetItems()))
	for _, item := range snapshot.GetItems() {
		unitPrice := item.GetUnitPrice()
		lineTotal := item.GetLineTotal()
		if unitPrice == nil || lineTotal == nil {
			return outbound.CheckoutSnapshot{}, errors.New("cart get checkout snapshot: invalid item money payload")
		}

		quantity, quantityErr := int64ToInt32(item.GetQuantity())
		if quantityErr != nil {
			return outbound.CheckoutSnapshot{}, fmt.Errorf("cart get checkout snapshot: %w", quantityErr)
		}

		productResponse, productErr := a.catalogClient.GetProductBySKU(withoutAuthorizationMetadata(ctx), &catalogv1.GetProductBySKURequest{Sku: item.GetSku()})
		if productErr != nil {
			if status.Code(productErr) == codes.NotFound {
				return outbound.CheckoutSnapshot{}, outbound.ErrStockReservationSKUNotFound
			}

			return outbound.CheckoutSnapshot{}, fmt.Errorf("catalog get product by sku: %w", productErr)
		}

		productIDRaw := ""
		if productResponse != nil && productResponse.GetProduct() != nil {
			productIDRaw = productResponse.GetProduct().GetProductId()
		}

		productID, parseErr := uuid.Parse(strings.TrimSpace(productIDRaw))
		if parseErr != nil || productID == uuid.Nil {
			return outbound.CheckoutSnapshot{}, errors.New("catalog get product by sku: invalid product id")
		}

		items = append(items, outbound.CheckoutSnapshotItem{
			ProductID: productID,
			SKU:       item.GetSku(),
			Name:      item.GetName(),
			Quantity:  quantity,
			UnitPrice: unitPrice.GetAmount(),
			LineTotal: lineTotal.GetAmount(),
			Currency:  lineTotal.GetCurrency(),
		})
	}

	return outbound.CheckoutSnapshot{
		UserID:      userID,
		Currency:    total.GetCurrency(),
		TotalAmount: total.GetAmount(),
		Items:       items,
	}, nil
}

func WithShopperAuthorization(ctx context.Context, authorization string) context.Context {
	authorization = strings.TrimSpace(authorization)
	if authorization == "" {
		return ctx
	}

	return context.WithValue(ctx, shopperAuthorizationContextKey{}, authorization)
}

func withAuthorizationMetadata(ctx context.Context) context.Context {
	authorization, ok := ctx.Value(shopperAuthorizationContextKey{}).(string)
	if !ok || strings.TrimSpace(authorization) == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, "authorization", authorization)
}

func withoutAuthorizationMetadata(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}

	filtered := md.Copy()
	delete(filtered, "authorization")

	return metadata.NewOutgoingContext(ctx, filtered)
}

type StockReservationService struct {
	client catalogv1.CatalogServiceClient
}

func NewStockReservationService(client catalogv1.CatalogServiceClient) *StockReservationService {
	return &StockReservationService{client: client}
}

func (a *StockReservationService) ReserveStock(ctx context.Context, input outbound.ReserveStockInput) error {
	items := make([]*catalogv1.ReservationItem, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, &catalogv1.ReservationItem{
			ProductId: item.ProductID.String(),
			Quantity:  int64(item.Quantity),
		})
	}

	_, err := a.client.ReserveStock(ctx, &catalogv1.ReserveStockRequest{
		OrderId: input.OrderID.String(),
		Items:   items,
	})
	if err == nil {
		return nil
	}

	switch status.Code(err) {
	case codes.NotFound:
		return outbound.ErrStockReservationSKUNotFound
	case codes.FailedPrecondition:
		return outbound.ErrStockReservationUnavailable
	case codes.Aborted:
		return outbound.ErrStockReservationConflict
	default:
		return fmt.Errorf("catalog reserve stock: %w", err)
	}
}

type StockReleaseService struct {
	client catalogv1.CatalogServiceClient
}

func NewStockReleaseService(client catalogv1.CatalogServiceClient) *StockReleaseService {
	return &StockReleaseService{client: client}
}

func (a *StockReleaseService) ReleaseStock(ctx context.Context, input outbound.ReleaseStockInput) error {
	_, err := a.client.ReleaseStock(ctx, &catalogv1.ReleaseStockRequest{OrderId: input.OrderID.String()})
	if err == nil {
		return nil
	}

	switch status.Code(err) {
	case codes.NotFound:
		return outbound.ErrStockReleaseNotFound
	case codes.FailedPrecondition:
		return outbound.ErrStockReleaseUnavailable
	case codes.Aborted:
		return outbound.ErrStockReleaseConflict
	default:
		return fmt.Errorf("catalog release stock: %w", err)
	}
}

type CheckoutPaymentService struct {
	client paymentv1.PaymentServiceClient
}

func NewCheckoutPaymentService(client paymentv1.PaymentServiceClient) *CheckoutPaymentService {
	return &CheckoutPaymentService{client: client}
}

func (a *CheckoutPaymentService) InitiatePayment(ctx context.Context, input outbound.InitiatePaymentInput) error {
	response, err := a.client.InitiatePayment(ctx, &paymentv1.InitiatePaymentRequest{
		OrderId: input.OrderID.String(),
		Amount: &commonv1.Money{
			Amount:   input.Amount,
			Currency: input.Currency,
		},
		ProviderName:   input.PaymentProvider,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.FailedPrecondition:
			return outbound.ErrPaymentDeclined
		case codes.Aborted:
			return outbound.ErrPaymentConflict
		default:
			return fmt.Errorf("payment initiate payment: %w", err)
		}
	}

	attempt := response.GetPaymentAttempt()
	if attempt == nil {
		return errors.New("payment initiate payment: empty payment attempt")
	}

	if attempt.GetStatus() == paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED {
		return outbound.ErrPaymentDeclined
	}

	if attempt.GetStatus() != paymentv1.PaymentStatus_PAYMENT_STATUS_SUCCEEDED {
		return outbound.ErrPaymentConflict
	}

	return nil
}

func int64ToInt32(v int64) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("quantity out of int32 range: %d", v)
	}

	return int32(v), nil
}

var _ outbound.CheckoutSnapshotRepository = (*CheckoutSnapshotRepository)(nil)
var _ outbound.StockReservationService = (*StockReservationService)(nil)
var _ outbound.StockReleaseService = (*StockReleaseService)(nil)
var _ outbound.CheckoutPaymentService = (*CheckoutPaymentService)(nil)
