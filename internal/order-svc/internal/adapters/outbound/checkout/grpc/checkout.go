package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	catalogv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/catalog/v1"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

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
	response, err := a.cartClient.GetCheckoutSnapshot(ctx, &cartv1.GetCheckoutSnapshotRequest{UserId: userID.String()})
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

		productResponse, productErr := a.catalogClient.GetProductBySKU(ctx, &catalogv1.GetProductBySKURequest{Sku: item.GetSku()})
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
			Quantity:  int32(item.GetQuantity()),
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

var _ outbound.CheckoutSnapshotRepository = (*CheckoutSnapshotRepository)(nil)
var _ outbound.StockReservationService = (*StockReservationService)(nil)
