//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	commonv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/common/v1"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	paymentv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/payment/v1"
	commonoutbox "github.com/shrtyk/e-commerce-platform/internal/common/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/inbound/http/dto"
	adapteroutbox "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/adapters/outbound/postgres/outbox"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/testhelper"
)

func TestCreateOrderHTTPPersistsOrder(t *testing.T) {
	stack := newCleanOrderStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	productID := uuid.New()
	idempotencyKey := "idem-happy-path"

	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-INT-1",
			Name:      "Integration Product",
			Quantity:  2,
			UnitPrice: &commonv1.Money{Amount: 1500, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 3000, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 3000, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: productID.String(),
		SKU:       "SKU-INT-1",
		Name:      "Integration Product",
		Price:     1500,
		Currency:  "USD",
	})

	order := createOrderHTTP(t, stack, token, idempotencyKey, dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusAccepted)
	require.Equal(t, userID.String(), order.UserId)
	require.Equal(t, dto.AwaitingPayment, order.Status)
	require.Equal(t, 3000, order.TotalAmount)
	require.Len(t, order.Items, 1)
	require.Equal(t, productID.String(), order.Items[0].ProductId)
	require.Equal(t, "SKU-INT-1", stringValue(order.Items[0].Sku))

	dbOrder := readOrderRow(t, stack.DB, order.OrderId)
	require.Equal(t, userID, dbOrder.UserID)
	require.Equal(t, "awaiting_payment", dbOrder.Status)
	require.Equal(t, int64(3000), dbOrder.TotalAmount)

	saga := readSagaStateRow(t, stack.DB, order.OrderId)
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "requested", saga.PaymentStage)
	require.False(t, saga.LastErrorCode.Valid)

	history := readStatusHistoryRows(t, stack.DB, order.OrderId)
	require.Len(t, history, 2)
	require.Equal(t, "pending", history[0].FromStatus.String)
	require.Equal(t, "awaiting_stock", history[0].ToStatus)
	require.Equal(t, "awaiting_stock", history[1].FromStatus.String)
	require.Equal(t, "awaiting_payment", history[1].ToStatus)

	outbox := readOutboxEvents(t, stack.DB)
	require.Len(t, outbox, 1)
	require.Equal(t, "order.created", outbox[0].EventName)
	require.Equal(t, order.OrderId, outbox[0].AggregateID)
	require.Equal(t, "pending", outbox[0].Status)
	require.Equal(t, idempotencyKey, outbox[0].Headers["causationId"])
	require.Equal(t, idempotencyKey, outbox[0].Headers["idempotencyKey"])

	requests := stack.PaymentServer.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, order.OrderId, requests[0].GetOrderId())
	require.Equal(t, int64(3000), requests[0].GetAmount().GetAmount())
	require.NotEmpty(t, requests[0].GetIdempotencyKey())
}

func TestCreateOrderHTTPHandlesIdempotencyReplay(t *testing.T) {
	stack := newCleanOrderStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-INT-2",
			Name:      "Replay Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 2200, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 2200, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 2200, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-INT-2",
		Name:      "Replay Product",
		Price:     2200,
		Currency:  "USD",
	})

	first := createOrderHTTP(t, stack, token, "idem-replay", dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusAccepted)
	second := createOrderHTTP(t, stack, token, "idem-replay", dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusAccepted)
	require.Equal(t, first.OrderId, second.OrderId)

	httpErr := createOrderHTTPError(t, stack, token, "idem-replay", dto.CreateOrderRequest{PaymentMethod: stringPtr("bank_transfer")}, http.StatusConflict)
	require.Equal(t, "IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD", httpErr.Code)

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM order_checkout_idempotency`))
	require.Len(t, stack.PaymentServer.Requests(), 1)
}

func TestCreateOrderHTTPCompensatesOnPaymentFailure(t *testing.T) {
	stack := newCleanOrderStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-INT-3",
			Name:      "Failure Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 1800, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 1800, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 1800, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-INT-3",
		Name:      "Failure Product",
		Price:     1800,
		Currency:  "USD",
	})
	stack.PaymentServer.SetResult(&paymentv1.PaymentAttempt{
		PaymentAttemptId: uuid.NewString(),
		Status:           paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED,
		ProviderName:     "card",
		Amount:           &commonv1.Money{Amount: 1800, Currency: "USD"},
	}, nil)

	httpErr := createOrderHTTPError(t, stack, token, "idem-payment-fail", dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusConflict)
	require.Equal(t, "PAYMENT_DECLINED", httpErr.Code)

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders WHERE status = 'cancelled'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.cancelled'`))

	saga := readSingleSagaStateRow(t, stack.DB)
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "failed", saga.PaymentStage)
	require.Equal(t, "PAYMENT_DECLINED", saga.LastErrorCode.String)

	releaseCalls := stack.CatalogServer.ReleaseCalls()
	require.Len(t, releaseCalls, 1)
}

func TestCreateOrderHTTPTreatsNonTerminalPaymentStatusAsConflict(t *testing.T) {
	stack := newCleanOrderStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-INT-PROC",
			Name:      "Processing Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 1900, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 1900, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 1900, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-INT-PROC",
		Name:      "Processing Product",
		Price:     1900,
		Currency:  "USD",
	})
	stack.PaymentServer.SetResult(&paymentv1.PaymentAttempt{
		PaymentAttemptId: uuid.NewString(),
		Status:           paymentv1.PaymentStatus_PAYMENT_STATUS_PROCESSING,
		ProviderName:     "card",
		Amount:           &commonv1.Money{Amount: 1900, Currency: "USD"},
	}, nil)

	httpErr := createOrderHTTPError(t, stack, token, "idem-payment-processing", dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusConflict)
	require.Equal(t, "CONFLICT", httpErr.Code)

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders WHERE status = 'cancelled'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.cancelled'`))

	saga := readSingleSagaStateRow(t, stack.DB)
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "failed", saga.PaymentStage)
	require.Equal(t, "CONFLICT", saga.LastErrorCode.String)
}

func TestCreateOrderGRPCPersistsOrder(t *testing.T) {
	stack, client := newCleanOrderGRPCStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	productID := uuid.New()
	idempotencyKey := "idem-grpc-happy-path"

	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-GRPC-1",
			Name:      "Integration Product",
			Quantity:  2,
			UnitPrice: &commonv1.Money{Amount: 1500, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 3000, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 3000, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: productID.String(),
		SKU:       "SKU-GRPC-1",
		Name:      "Integration Product",
		Price:     1500,
		Currency:  "USD",
	})

	res, err := client.CreateOrder(grpcAuthContext(token), &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: idempotencyKey,
	})
	require.NoError(t, err)
	require.NotNil(t, res.GetOrder())
	require.Equal(t, userID.String(), res.GetOrder().GetUserId())
	require.Equal(t, orderv1.OrderStatus_ORDER_STATUS_AWAITING_PAYMENT, res.GetOrder().GetStatus())
	require.Equal(t, int64(3000), res.GetOrder().GetTotalAmount().GetAmount())
	require.Len(t, res.GetOrder().GetItems(), 1)
	require.Equal(t, productID.String(), res.GetOrder().GetItems()[0].GetProductId())
	require.Equal(t, "SKU-GRPC-1", res.GetOrder().GetItems()[0].GetSku())

	dbOrder := readOrderRow(t, stack.DB, res.GetOrder().GetOrderId())
	require.Equal(t, userID, dbOrder.UserID)
	require.Equal(t, "awaiting_payment", dbOrder.Status)
	require.Equal(t, int64(3000), dbOrder.TotalAmount)

	saga := readSagaStateRow(t, stack.DB, res.GetOrder().GetOrderId())
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "requested", saga.PaymentStage)
	require.False(t, saga.LastErrorCode.Valid)

	history := readStatusHistoryRows(t, stack.DB, res.GetOrder().GetOrderId())
	require.Len(t, history, 2)
	require.Equal(t, "pending", history[0].FromStatus.String)
	require.Equal(t, "awaiting_stock", history[0].ToStatus)
	require.Equal(t, "awaiting_stock", history[1].FromStatus.String)
	require.Equal(t, "awaiting_payment", history[1].ToStatus)

	outbox := readOutboxEvents(t, stack.DB)
	require.Len(t, outbox, 1)
	require.Equal(t, "order.created", outbox[0].EventName)
	require.Equal(t, res.GetOrder().GetOrderId(), outbox[0].AggregateID)
	require.Equal(t, "pending", outbox[0].Status)
	require.Equal(t, idempotencyKey, outbox[0].Headers["causationId"])
	require.Equal(t, idempotencyKey, outbox[0].Headers["idempotencyKey"])

	requests := stack.PaymentServer.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, res.GetOrder().GetOrderId(), requests[0].GetOrderId())
	require.Equal(t, int64(3000), requests[0].GetAmount().GetAmount())
	require.NotEmpty(t, requests[0].GetIdempotencyKey())
}

func TestCreateOrderGRPCHandlesIdempotencyReplay(t *testing.T) {
	stack, client := newCleanOrderGRPCStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-GRPC-2",
			Name:      "Replay Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 2200, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 2200, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 2200, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-GRPC-2",
		Name:      "Replay Product",
		Price:     2200,
		Currency:  "USD",
	})

	ctx := grpcAuthContext(token)
	first, err := client.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: "idem-grpc-replay",
	})
	require.NoError(t, err)

	second, err := client.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: "idem-grpc-replay",
	})
	require.NoError(t, err)
	require.Equal(t, first.GetOrder().GetOrderId(), second.GetOrder().GetOrderId())

	_, err = client.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "bank_transfer",
		IdempotencyKey: "idem-grpc-replay",
	})
	require.Error(t, err)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Equal(t, "IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD", status.Convert(err).Message())

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM order_checkout_idempotency`))
	require.Len(t, stack.PaymentServer.Requests(), 1)
}

func TestCreateOrderGRPCCompensatesOnPaymentFailure(t *testing.T) {
	stack, client := newCleanOrderGRPCStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	productID := uuid.NewString()
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-GRPC-3",
			Name:      "Failure Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 1800, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 1800, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 1800, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: productID,
		SKU:       "SKU-GRPC-3",
		Name:      "Failure Product",
		Price:     1800,
		Currency:  "USD",
	})
	stack.PaymentServer.SetResult(&paymentv1.PaymentAttempt{
		PaymentAttemptId: uuid.NewString(),
		Status:           paymentv1.PaymentStatus_PAYMENT_STATUS_FAILED,
		ProviderName:     "card",
		Amount:           &commonv1.Money{Amount: 1800, Currency: "USD"},
	}, nil)

	_, err := client.CreateOrder(grpcAuthContext(token), &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: "idem-grpc-payment-fail",
	})
	require.Error(t, err)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Equal(t, "PAYMENT_DECLINED", status.Convert(err).Message())

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders WHERE status = 'cancelled'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.cancelled'`))

	saga := readSingleSagaStateRow(t, stack.DB)
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "failed", saga.PaymentStage)
	require.Equal(t, "PAYMENT_DECLINED", saga.LastErrorCode.String)

	releaseCalls := stack.CatalogServer.ReleaseCalls()
	require.Len(t, releaseCalls, 1)

	cancelledOrder := readSingleOrderRowByStatus(t, stack.DB, "cancelled")
	require.Equal(t, cancelledOrder.OrderID.String(), releaseCalls[0].GetOrderId())

	items := readOrderItemsByOrderID(t, stack.DB, cancelledOrder.OrderID.String())
	require.Len(t, items, 1)
	require.Equal(t, productID, items[0].ProductID.String())
	require.Equal(t, "SKU-GRPC-3", items[0].SKU)
	require.Equal(t, int32(1), items[0].Quantity)
	require.Equal(t, int64(1800), items[0].UnitPrice)
	require.Equal(t, int64(1800), items[0].LineTotal)
}

func TestCreateOrderGRPCTreatsNonTerminalPaymentStatusAsConflict(t *testing.T) {
	stack, client := newCleanOrderGRPCStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	productID := uuid.NewString()
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-GRPC-PROC",
			Name:      "Processing Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 1900, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 1900, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 1900, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: productID,
		SKU:       "SKU-GRPC-PROC",
		Name:      "Processing Product",
		Price:     1900,
		Currency:  "USD",
	})
	stack.PaymentServer.SetResult(&paymentv1.PaymentAttempt{
		PaymentAttemptId: uuid.NewString(),
		Status:           paymentv1.PaymentStatus_PAYMENT_STATUS_PROCESSING,
		ProviderName:     "card",
		Amount:           &commonv1.Money{Amount: 1900, Currency: "USD"},
	}, nil)

	_, err := client.CreateOrder(grpcAuthContext(token), &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: "idem-grpc-payment-processing",
	})
	require.Error(t, err)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Equal(t, "CONFLICT", status.Convert(err).Message())

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders WHERE status = 'cancelled'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.cancelled'`))

	saga := readSingleSagaStateRow(t, stack.DB)
	require.Equal(t, "succeeded", saga.StockStage)
	require.Equal(t, "failed", saga.PaymentStage)
	require.Equal(t, "CONFLICT", saga.LastErrorCode.String)
}

func TestGetOrderGRPCReturnsOrderAndNotFound(t *testing.T) {
	stack, client := newCleanOrderGRPCStack(t)

	userID := uuid.New()
	userToken := testhelper.MintAccessToken(t, userID)
	otherUserToken := testhelper.MintAccessToken(t, uuid.New())

	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-GRPC-4",
			Name:      "Get Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 2500, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 2500, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 2500, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-GRPC-4",
		Name:      "Get Product",
		Price:     2500,
		Currency:  "USD",
	})

	created, err := client.CreateOrder(grpcAuthContext(userToken), &orderv1.CreateOrderRequest{
		UserId:         userID.String(),
		PaymentMethod:  "card",
		IdempotencyKey: "idem-grpc-get",
	})
	require.NoError(t, err)

	happy, err := client.GetOrder(grpcAuthContext(userToken), &orderv1.GetOrderRequest{
		OrderId: created.GetOrder().GetOrderId(),
		UserId:  userID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, created.GetOrder().GetOrderId(), happy.GetOrder().GetOrderId())
	require.Equal(t, userID.String(), happy.GetOrder().GetUserId())
	require.Equal(t, orderv1.OrderStatus_ORDER_STATUS_AWAITING_PAYMENT, happy.GetOrder().GetStatus())

	_, err = client.GetOrder(grpcAuthContext(otherUserToken), &orderv1.GetOrderRequest{
		OrderId: created.GetOrder().GetOrderId(),
		UserId:  userID.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, "CART_NOT_FOUND", status.Convert(err).Message())

	_, err = client.GetOrder(grpcAuthContext(userToken), &orderv1.GetOrderRequest{
		OrderId: uuid.NewString(),
		UserId:  userID.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, "CART_NOT_FOUND", status.Convert(err).Message())
}

func TestGetOrderHTTPCoversAuthOwnershipAndValidation(t *testing.T) {
	stack := newCleanOrderStack(t)

	ownerID := uuid.New()
	ownerToken := testhelper.MintAccessToken(t, ownerID)
	otherToken := testhelper.MintAccessToken(t, uuid.New())

	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   ownerID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-HTTP-GET-1",
			Name:      "HTTP Get Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 1700, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 1700, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 1700, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-HTTP-GET-1",
		Name:      "HTTP Get Product",
		Price:     1700,
		Currency:  "USD",
	})

	created := createOrderHTTP(t, stack, ownerToken, "idem-http-get", dto.CreateOrderRequest{PaymentMethod: stringPtr("card")}, http.StatusAccepted)

	happy := getOrderHTTP(t, stack, ownerToken, created.OrderId, http.StatusOK)
	require.Equal(t, created.OrderId, happy.OrderId)
	require.Equal(t, ownerID.String(), happy.UserId)
	require.Equal(t, dto.AwaitingPayment, happy.Status)

	notFoundForOtherUser := getOrderHTTPError(t, stack, otherToken, created.OrderId, http.StatusNotFound)
	require.Equal(t, "CART_NOT_FOUND", notFoundForOtherUser.Code)

	missingAuthReq := testhelper.HTTPJSONRequest(t, http.MethodGet, "/v1/orders/"+created.OrderId, "", nil)
	missingAuthRes := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(missingAuthRes, missingAuthReq)
	require.Equal(t, http.StatusUnauthorized, missingAuthRes.Code)

	invalidAuthReq := testhelper.HTTPJSONRequest(t, http.MethodGet, "/v1/orders/"+created.OrderId, "not-a-jwt", nil)
	invalidAuthRes := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(invalidAuthRes, invalidAuthReq)
	require.Equal(t, http.StatusUnauthorized, invalidAuthRes.Code)

	invalidUUID := getOrderHTTPError(t, stack, ownerToken, "not-a-uuid", http.StatusBadRequest)
	require.Equal(t, "INVALID_ARGUMENT", invalidUUID.Code)
}

func TestCreateOrderHTTPIdempotencyRaceCreatesSingleOrder(t *testing.T) {
	stack := newCleanOrderStack(t)

	userID := uuid.New()
	token := testhelper.MintAccessToken(t, userID)
	stack.CartServer.SetSnapshot(&cartv1.CheckoutSnapshot{
		UserId:   userID.String(),
		Currency: "USD",
		Items: []*cartv1.CartItem{{
			Sku:       "SKU-HTTP-RACE-1",
			Name:      "Race Product",
			Quantity:  1,
			UnitPrice: &commonv1.Money{Amount: 900, Currency: "USD"},
			LineTotal: &commonv1.Money{Amount: 900, Currency: "USD"},
		}},
		TotalAmount: &commonv1.Money{Amount: 900, Currency: "USD"},
	})
	stack.CatalogServer.UpsertProduct(testhelper.CatalogProduct{
		ProductID: uuid.NewString(),
		SKU:       "SKU-HTTP-RACE-1",
		Name:      "Race Product",
		Price:     900,
		Currency:  "USD",
	})

	const attempts = 8
	const idemKey = "idem-http-race"

	type raceResult struct {
		status    int
		orderID   string
		errCode   string
		decodeErr error
	}

	start := make(chan struct{})
	results := make(chan raceResult, attempts)
	var wg sync.WaitGroup

	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			req := testhelper.HTTPJSONRequest(t, http.MethodPost, "/v1/orders", token, dto.CreateOrderRequest{PaymentMethod: stringPtr("card")})
			req.Header.Set("Idempotency-Key", idemKey)

			res := httptest.NewRecorder()
			stack.HTTPHandler.ServeHTTP(res, req)

			result := raceResult{status: res.Code}
			if res.Code == http.StatusAccepted {
				var order dto.Order
				result.decodeErr = json.NewDecoder(res.Body).Decode(&order)
				result.orderID = order.OrderId
			} else {
				var errResp dto.ErrorResponse
				result.decodeErr = json.NewDecoder(res.Body).Decode(&errResp)
				result.errCode = errResp.Code
			}

			results <- result
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	var createdOrderID string
	for result := range results {
		require.NoError(t, result.decodeErr)
		require.Equal(t, http.StatusAccepted, result.status)
		require.Empty(t, result.errCode)
		require.NotEmpty(t, result.orderID)

		if createdOrderID == "" {
			createdOrderID = result.orderID
		}
		require.Equal(t, createdOrderID, result.orderID)
	}

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM orders`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM order_checkout_idempotency`))
	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE event_name = 'order.created'`))
	require.Len(t, stack.PaymentServer.Requests(), 1)
}

func TestOutboxRepositoryClaimAndOwnershipSemanticsUnderContention(t *testing.T) {
	stack := newCleanOrderStack(t)
	repo := adapteroutbox.NewRepository(stack.DB)

	for i := 0; i < 3; i++ {
		_, err := repo.Append(context.Background(), commonoutbox.Record{
			EventID:       uuid.NewString(),
			EventName:     "order.created",
			AggregateType: "order",
			AggregateID:   uuid.NewString(),
			Topic:         "order.events",
			Payload:       []byte{byte('a' + i)},
			Headers:       map[string]string{"source": "integration"},
			Status:        commonoutbox.StatusPending,
		})
		require.NoError(t, err)
	}

	type claimResult struct {
		worker  string
		records []commonoutbox.Record
		err     error
	}

	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup

	workers := []string{"worker-a", "worker-b"}
	for _, worker := range workers {
		workerName := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			records, err := repo.ClaimPending(context.Background(), commonoutbox.ClaimPendingParams{
				Limit:    2,
				Before:   time.Now().UTC().Add(time.Second),
				LockedBy: workerName,
			})

			results <- claimResult{worker: workerName, records: records, err: err}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	claimedByWorker := make(map[string][]commonoutbox.Record, 2)
	claimedIDs := make(map[uuid.UUID]string, 3)
	for result := range results {
		require.NoError(t, result.err)
		claimedByWorker[result.worker] = result.records
		for _, record := range result.records {
			owner, exists := claimedIDs[record.ID]
			require.Falsef(t, exists, "record %s claimed twice by %s and %s", record.ID, owner, result.worker)
			claimedIDs[record.ID] = result.worker
		}
	}

	require.Len(t, claimedIDs, 3)

	var ownerWorker string
	var ownerRecord commonoutbox.Record
	for worker, records := range claimedByWorker {
		if len(records) == 0 {
			continue
		}

		ownerWorker = worker
		ownerRecord = records[0]
		break
	}
	require.NotEmpty(t, ownerWorker)

	intruderWorker := "worker-a"
	if ownerWorker == intruderWorker {
		intruderWorker = "worker-b"
	}

	err := repo.MarkPublished(context.Background(), commonoutbox.MarkPublishedParams{
		ID:          ownerRecord.ID,
		ClaimToken:  ownerRecord.LockedAt,
		LockedBy:    intruderWorker,
		PublishedAt: time.Now().UTC(),
	})
	require.ErrorIs(t, err, commonoutbox.ErrPublishConflict)

	err = repo.MarkPublished(context.Background(), commonoutbox.MarkPublishedParams{
		ID:          ownerRecord.ID,
		ClaimToken:  ownerRecord.LockedAt,
		LockedBy:    ownerWorker,
		PublishedAt: time.Now().UTC(),
	})
	require.NoError(t, err)

	require.Equal(t, 1, countRows(t, stack.DB, `SELECT COUNT(*) FROM outbox_records WHERE status = 'published'`))
}

type orderRow struct {
	OrderID     uuid.UUID
	UserID      uuid.UUID
	Status      string
	TotalAmount int64
}

type sagaStateRow struct {
	StockStage    string
	PaymentStage  string
	LastErrorCode sql.NullString
}

type statusHistoryRow struct {
	FromStatus sql.NullString
	ToStatus   string
}

type outboxEventRow struct {
	EventName   string
	AggregateID string
	Status      string
	Headers     map[string]string
}

type orderItemRow struct {
	ProductID uuid.UUID
	SKU       string
	Quantity  int32
	UnitPrice int64
	LineTotal int64
}

func newCleanOrderStack(t *testing.T) *testhelper.TestStack {
	t.Helper()
	harness := testhelper.IntegrationHarness(t)
	testhelper.CleanupDB(t, harness.DB)
	return testhelper.NewTestStack(t, harness.DB)
}

func newCleanOrderGRPCStack(t *testing.T) (*testhelper.TestStack, orderv1.OrderServiceClient) {
	t.Helper()
	stack := newCleanOrderStack(t)
	return stack, orderv1.NewOrderServiceClient(stack.GRPCConn)
}

func createOrderHTTP(t *testing.T, stack *testhelper.TestStack, token string, idempotencyKey string, body dto.CreateOrderRequest, expectedStatus int) dto.Order {
	t.Helper()
	req := testhelper.HTTPJSONRequest(t, http.MethodPost, "/v1/orders", token, body)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	res := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(res, req)
	require.Equal(t, expectedStatus, res.Code)

	var result dto.Order
	require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
	return result
}

func createOrderHTTPError(t *testing.T, stack *testhelper.TestStack, token string, idempotencyKey string, body dto.CreateOrderRequest, expectedStatus int) dto.ErrorResponse {
	t.Helper()
	req := testhelper.HTTPJSONRequest(t, http.MethodPost, "/v1/orders", token, body)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	res := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(res, req)
	require.Equal(t, expectedStatus, res.Code)

	var result dto.ErrorResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
	return result
}

func getOrderHTTP(t *testing.T, stack *testhelper.TestStack, token string, orderID string, expectedStatus int) dto.Order {
	t.Helper()
	req := testhelper.HTTPJSONRequest(t, http.MethodGet, "/v1/orders/"+orderID, token, nil)
	res := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(res, req)
	require.Equal(t, expectedStatus, res.Code)

	var result dto.Order
	require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
	return result
}

func getOrderHTTPError(t *testing.T, stack *testhelper.TestStack, token string, orderID string, expectedStatus int) dto.ErrorResponse {
	t.Helper()
	req := testhelper.HTTPJSONRequest(t, http.MethodGet, "/v1/orders/"+orderID, token, nil)
	res := httptest.NewRecorder()
	stack.HTTPHandler.ServeHTTP(res, req)
	require.Equal(t, expectedStatus, res.Code)

	var result dto.ErrorResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
	return result
}

func readOrderRow(t *testing.T, db *sql.DB, orderID string) orderRow {
	t.Helper()
	var row orderRow
	err := db.QueryRowContext(context.Background(), `SELECT order_id, user_id, status::text, total_amount FROM orders WHERE order_id = $1`, orderID).
		Scan(&row.OrderID, &row.UserID, &row.Status, &row.TotalAmount)
	require.NoError(t, err)
	return row
}

func readSagaStateRow(t *testing.T, db *sql.DB, orderID string) sagaStateRow {
	t.Helper()
	var row sagaStateRow
	err := db.QueryRowContext(context.Background(), `SELECT stock_stage::text, payment_stage::text, last_error_code FROM order_saga_state WHERE order_id = $1`, orderID).
		Scan(&row.StockStage, &row.PaymentStage, &row.LastErrorCode)
	require.NoError(t, err)
	return row
}

func readSingleSagaStateRow(t *testing.T, db *sql.DB) sagaStateRow {
	t.Helper()
	var row sagaStateRow
	err := db.QueryRowContext(context.Background(), `SELECT stock_stage::text, payment_stage::text, last_error_code FROM order_saga_state LIMIT 1`).
		Scan(&row.StockStage, &row.PaymentStage, &row.LastErrorCode)
	require.NoError(t, err)
	return row
}

func readSingleOrderRowByStatus(t *testing.T, db *sql.DB, orderStatus string) orderRow {
	t.Helper()
	var row orderRow
	err := db.QueryRowContext(
		context.Background(),
		`SELECT order_id, user_id, status::text, total_amount FROM orders WHERE status = $1 ORDER BY created_at ASC LIMIT 1`,
		orderStatus,
	).Scan(&row.OrderID, &row.UserID, &row.Status, &row.TotalAmount)
	require.NoError(t, err)
	return row
}

func readOrderItemsByOrderID(t *testing.T, db *sql.DB, orderID string) []orderItemRow {
	t.Helper()
	rows, err := db.QueryContext(
		context.Background(),
		`SELECT product_id, sku, quantity, unit_price, line_total FROM order_items WHERE order_id = $1 ORDER BY created_at ASC, order_item_id ASC`,
		orderID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var result []orderItemRow
	for rows.Next() {
		var row orderItemRow
		require.NoError(t, rows.Scan(&row.ProductID, &row.SKU, &row.Quantity, &row.UnitPrice, &row.LineTotal))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func readStatusHistoryRows(t *testing.T, db *sql.DB, orderID string) []statusHistoryRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT from_status, to_status FROM order_status_history WHERE order_id = $1 ORDER BY created_at ASC, order_status_history_id ASC`, orderID)
	require.NoError(t, err)
	defer rows.Close()

	var result []statusHistoryRow
	for rows.Next() {
		var row statusHistoryRow
		require.NoError(t, rows.Scan(&row.FromStatus, &row.ToStatus))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func readOutboxEvents(t *testing.T, db *sql.DB) []outboxEventRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT event_name, aggregate_id, status::text, headers FROM outbox_records ORDER BY created_at ASC`)
	require.NoError(t, err)
	defer rows.Close()

	var result []outboxEventRow
	for rows.Next() {
		var row outboxEventRow
		var headersRaw []byte
		require.NoError(t, rows.Scan(&row.EventName, &row.AggregateID, &row.Status, &headersRaw))
		require.NoError(t, json.Unmarshal(headersRaw, &row.Headers))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(context.Background(), query).Scan(&count))
	return count
}

func stringPtr(value string) *string {
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func grpcAuthContext(token string) context.Context {
	return metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+token),
	)
}
