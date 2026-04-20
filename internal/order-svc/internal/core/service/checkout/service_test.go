package checkout

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound/mocks"
)

func TestCheckoutInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CheckoutInput
	}{
		{
			name: "nil user id",
			input: CheckoutInput{
				UserID:         uuid.Nil,
				IdempotencyKey: "idem-1",
			},
		},
		{
			name: "blank idempotency key",
			input: CheckoutInput{
				UserID:         uuid.New(),
				IdempotencyKey: "   ",
			},
		},
		{
			name: "idempotency key too long",
			input: CheckoutInput{
				UserID:         uuid.New(),
				IdempotencyKey: strings.Repeat("x", 256),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			orders := outboundmocks.NewMockOrderRepository(t)
			saga := outboundmocks.NewMockOrderSagaStateRepository(t)
			snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
			stock := outboundmocks.NewMockStockReservationService(t)

			svc := NewService(orders, saga, snapshots, stock, nil, nil)

			order, err := svc.Checkout(context.Background(), tt.input)
			require.Equal(t, outbound.Order{}, order)
			require.Error(t, err)
			require.Equal(t, CheckoutErrorCodeInvalidArgument, CodeOf(err))

			orders.AssertNotCalled(t, "GetByUserIDAndIdempotencyKey", testifymock.Anything, testifymock.Anything, testifymock.Anything)
			snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
			stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
		})
	}
}

func TestCheckoutIdempotencyReplay(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-1"
	existingOrder := outbound.Order{
		OrderID:     uuid.New(),
		UserID:      userID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 1500,
		Items: []outbound.OrderItem{{
			OrderItemID: uuid.New(),
			OrderID:     uuid.New(),
			ProductID:   uuid.New(),
			SKU:         "SKU-1",
			Name:        "Item 1",
			Quantity:    1,
			UnitPrice:   1500,
			LineTotal:   1500,
			Currency:    "USD",
		}},
	}

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(existingOrder, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey})
	require.NoError(t, err)
	require.Equal(t, existingOrder, order)

	orders.AssertNumberOfCalls(t, "GetByUserIDAndIdempotencyKey", 1)
	snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutIdempotencyReplaySamePayloadStillSucceeds(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-1"
	paymentMethod := "  CARD  "
	existingOrder := outbound.Order{OrderID: uuid.New(), UserID: userID, Status: outbound.OrderStatusAwaitingPayment}

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(existingOrder, nil).
		Once()
	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ValidateCheckoutIdempotencyInput) bool {
			return input.UserID == userID && input.IdempotencyKey == idempotencyKey && input.Payload.PaymentMethod == "card"
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{
		UserID:         userID,
		IdempotencyKey: idempotencyKey,
		PaymentMethod:  &paymentMethod,
	})
	require.NoError(t, err)
	require.Equal(t, existingOrder, order)

	guard.AssertNumberOfCalls(t, "ValidateCheckoutIdempotency", 1)
	snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutIdempotencyReplayDifferentPayloadFails(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-1"
	paymentMethod := "bank_transfer"
	existingOrder := outbound.Order{OrderID: uuid.New(), UserID: userID, Status: outbound.OrderStatusAwaitingPayment}

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(existingOrder, nil).
		Once()
	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ValidateCheckoutIdempotencyInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == idempotencyKey &&
				input.Payload.PaymentMethod == "bank_transfer"
		})).
		Return(outbound.ErrCheckoutIdempotencyPayloadMismatch).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{
		UserID:         userID,
		IdempotencyKey: idempotencyKey,
		PaymentMethod:  &paymentMethod,
	})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrCheckoutIdempotencyPayloadMismatch)
	require.Equal(t, CheckoutErrorCodeWrongIdempotencyKeyPayload, CodeOf(err))

	guard.AssertNumberOfCalls(t, "ValidateCheckoutIdempotency", 1)
	snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutIdempotencyReplayNonTerminalOrderDoesNotShortCircuit(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-non-terminal"
	paymentMethod := "card"

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	for range replayNonTerminalRetries + 1 {
		orders.EXPECT().
			GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
			Return(outbound.Order{OrderID: uuid.New(), UserID: userID, Status: outbound.OrderStatusAwaitingStock}, nil).
			Once()
	}

	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderInvalidStatusTransition)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))

	snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutIdempotencyReplayNonTerminalOrderRetriesAndReplaysWhenOrderProgresses(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-non-terminal-progress"
	paymentMethod := "card"
	orderID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock}, nil).
		Once()

	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ValidateCheckoutIdempotencyInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == idempotencyKey &&
				input.Payload.PaymentMethod == paymentMethod
		})).
		Return(nil).
		Once()

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.NoError(t, err)
	require.Equal(t, orderID, order.OrderID)
	require.Equal(t, outbound.OrderStatusAwaitingPayment, order.Status)

	snapshots.AssertNotCalled(t, "GetCheckoutSnapshot", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutRetryAfterMidLoopFailureReplaysCompensatedOrder(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	idempotencyKey := "idem-retry-after-mid-loop-fail"
	paymentMethod := "card"
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodeConflict)

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1600, "SKU-10", "Item", 1600), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 1600}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 1600, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()
	saga.EXPECT().
		TransitionStockStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeConflict)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted, LastErrorCode: &cancelReason}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1600, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingStock,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeConflict)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1600}, nil).
		Once()
	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ValidateCheckoutIdempotencyInput) bool {
			return input.UserID == userID && input.IdempotencyKey == idempotencyKey && input.Payload.PaymentMethod == paymentMethod
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil, guard)

	firstOrder, firstErr := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.Equal(t, outbound.Order{}, firstOrder)
	require.Error(t, firstErr)
	require.ErrorIs(t, firstErr, outbound.ErrOrderSagaStateInvalidTransition)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(firstErr))

	replayed, replayErr := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.NoError(t, replayErr)
	require.Equal(t, outbound.OrderStatusCancelled, replayed.Status)
	require.Equal(t, orderID, replayed.OrderID)

	snapshots.AssertNumberOfCalls(t, "GetCheckoutSnapshot", 1)
}

func TestCheckoutIdempotencyConflictReplaysExistingOrderForSamePayload(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-race"
	paymentMethod := "card"
	orderID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()

	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1000, "SKU-1", "Item", 1000), nil).
		Once()

	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{}, outbound.ErrOrderIdempotencyConflict).
		Once()

	replayed := outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment}
	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(replayed, nil).
		Once()

	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ValidateCheckoutIdempotencyInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == idempotencyKey &&
				input.Payload.PaymentMethod == paymentMethod
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.NoError(t, err)
	require.Equal(t, replayed, order)

	orders.AssertNotCalled(t, "TransitionStatus", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "TransitionStockStageToRequested", testifymock.Anything, testifymock.Anything)
	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutIdempotencyConflictReplayDifferentPayloadFails(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-race-mismatch"
	paymentMethod := "bank_transfer"

	orders := outboundmocks.NewMockOrderRepository(t)
	guard := outboundmocks.NewMockCheckoutIdempotencyGuard(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()

	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1000, "SKU-1", "Item", 1000), nil).
		Once()

	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{}, outbound.ErrOrderIdempotencyConflict).
		Once()

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{OrderID: uuid.New(), UserID: userID, Status: outbound.OrderStatusAwaitingPayment}, nil).
		Once()

	guard.EXPECT().
		ValidateCheckoutIdempotency(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrCheckoutIdempotencyPayloadMismatch).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, guard)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey, PaymentMethod: &paymentMethod})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrCheckoutIdempotencyPayloadMismatch)
	require.Equal(t, CheckoutErrorCodeWrongIdempotencyKeyPayload, CodeOf(err))
}

func TestCheckoutConcurrentIdempotencyReplaySamePayload(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	idempotencyKey := "idem-concurrent"
	paymentMethod := "card"

	orders := newConcurrentReplayOrderRepo(userID, idempotencyKey, paymentMethod)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1000, "SKU-1", "Item", 1000), nil).
		Maybe()

	svc := NewService(orders, saga, snapshots, stock, nil, nil, NewCheckoutIdempotencyGuard(orders))

	var wg sync.WaitGroup
	start := make(chan struct{})

	type result struct {
		order outbound.Order
		err   error
	}
	results := make([]result, 2)

	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			order, err := svc.Checkout(context.Background(), CheckoutInput{
				UserID:         userID,
				IdempotencyKey: idempotencyKey,
				PaymentMethod:  &paymentMethod,
			})
			results[idx] = result{order: order, err: err}
		}(i)
	}

	close(start)
	wg.Wait()

	for _, res := range results {
		require.NoError(t, res.err)
		require.Equal(t, orders.orderID, res.order.OrderID)
		require.Equal(t, userID, res.order.UserID)
	}

	createCalls := orders.createCallCount()
	require.GreaterOrEqual(t, createCalls, 1)
	require.LessOrEqual(t, createCalls, 2)

	stock.AssertNotCalled(t, "ReserveStock", testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "TransitionStockStageToRequested", testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "TransitionPaymentStageToRequested", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutCartErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		snapshotOut outbound.CheckoutSnapshot
		snapshotErr error
		expected    CheckoutErrorCode
	}{
		{
			name:        "cart not found",
			snapshotErr: outbound.ErrCheckoutSnapshotNotFound,
			expected:    CheckoutErrorCodeCartNotFound,
		},
		{
			name:        "cart empty",
			snapshotOut: outbound.CheckoutSnapshot{Currency: "USD", TotalAmount: 0, Items: nil},
			expected:    CheckoutErrorCodeCartEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			orders := outboundmocks.NewMockOrderRepository(t)
			saga := outboundmocks.NewMockOrderSagaStateRepository(t)
			snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
			stock := outboundmocks.NewMockStockReservationService(t)

			orders.EXPECT().
				GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-1").
				Return(outbound.Order{}, outbound.ErrOrderNotFound).
				Once()

			if tt.snapshotErr != nil {
				snapshots.EXPECT().
					GetCheckoutSnapshot(testifymock.Anything, userID).
					Return(outbound.CheckoutSnapshot{}, tt.snapshotErr).
					Once()
			} else {
				snapshots.EXPECT().
					GetCheckoutSnapshot(testifymock.Anything, userID).
					Return(outbound.CheckoutSnapshot{UserID: userID, Currency: tt.snapshotOut.Currency, TotalAmount: tt.snapshotOut.TotalAmount, Items: tt.snapshotOut.Items}, nil).
					Once()
			}

			svc := NewService(orders, saga, snapshots, stock, nil, nil)

			order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-1"})
			require.Equal(t, outbound.Order{}, order)
			require.Error(t, err)
			require.Equal(t, tt.expected, CodeOf(err))

			orders.AssertNotCalled(t, "CreateWithItems", testifymock.Anything, testifymock.Anything)
		})
	}
}

func TestCheckoutSuccessPathEndsAwaitingPayment(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	idempotencyKey := "idem-success"
	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)
	publisher := &captureEventPublisher{}

	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2500, "SKU-1", "Item 1", 2500), nil).
		Once()

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, idempotencyKey).
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.MatchedBy(func(input outbound.CreateOrderInput) bool {
			return input.UserID == userID &&
				input.Status == outbound.OrderStatusPending &&
				input.IdempotencyKey == idempotencyKey &&
				len(input.Items) == 1
		})).
		RunAndReturn(func(_ context.Context, input outbound.CreateOrderInput) (outbound.Order, error) {
			return outbound.Order{
				OrderID:     orderID,
				UserID:      userID,
				Status:      outbound.OrderStatusPending,
				Currency:    "USD",
				TotalAmount: 2500,
				Items: []outbound.OrderItem{{
					OrderItemID: uuid.New(),
					OrderID:     orderID,
					ProductID:   input.Items[0].ProductID,
					SKU:         input.Items[0].SKU,
					Name:        input.Items[0].Name,
					Quantity:    input.Items[0].Quantity,
					UnitPrice:   input.Items[0].UnitPrice,
					LineTotal:   input.Items[0].LineTotal,
					Currency:    input.Items[0].Currency,
				}},
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		}).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2500, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{OrderStatusHistoryID: uuid.New(), OrderID: orderID, FromStatus: &fromPending, ToStatus: outbound.OrderStatusAwaitingStock, CreatedAt: now}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2500, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{OrderStatusHistoryID: uuid.New(), OrderID: orderID, FromStatus: &fromAwaitingStock, ToStatus: outbound.OrderStatusAwaitingPayment, CreatedAt: now}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReserveStockInput) bool {
			return input.OrderID == orderID && input.UserID == userID && len(input.Items) == 1 && input.Items[0].Quantity == 1
		})).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.MatchedBy(func(input outbound.InitiatePaymentInput) bool {
			return input.OrderID == orderID &&
				input.Amount == 2500 &&
				input.Currency == "USD" &&
				input.IdempotencyKey == orderID.String() &&
				input.PaymentProvider == "default"
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, payment).WithEventing(publisher, nil, "order-svc")

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: idempotencyKey})
	require.NoError(t, err)
	require.Equal(t, outbound.OrderStatusAwaitingPayment, order.Status)
	require.Len(t, publisher.events, 1)
	require.Equal(t, "order.created", publisher.events[0].EventName)
	createdPayload, ok := publisher.events[0].Payload.(domain.OrderCreatedPayload)
	require.True(t, ok)
	require.Equal(t, domain.OrderStatusPending, createdPayload.Status)
	require.Equal(t, idempotencyKey, publisher.events[0].CausationID)

	orders.AssertNumberOfCalls(t, "TransitionStatus", 2)
	orders.AssertNumberOfCalls(t, "AppendStatusHistory", 2)
	saga.AssertNumberOfCalls(t, "TransitionStockStageToRequested", 1)
	saga.AssertNumberOfCalls(t, "TransitionStockStageToSucceeded", 1)
	saga.AssertNumberOfCalls(t, "TransitionPaymentStageToRequested", 1)
	saga.AssertNotCalled(t, "TransitionPaymentStageToSucceeded", testifymock.Anything, testifymock.Anything)
	stock.AssertNumberOfCalls(t, "ReserveStock", 1)
	payment.AssertNumberOfCalls(t, "InitiatePayment", 1)
	saga.AssertNotCalled(t, "TransitionStockStageToFailed", testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "TransitionPaymentStageToFailed", testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "SetLastErrorCode", testifymock.Anything, testifymock.Anything, testifymock.Anything)
}

func TestCheckoutPaymentInitiateSuccessDoesNotPublishOrderConfirmed(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-confirmed-publish-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2600, "SKU-4", "Item 4", 2600), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2600}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2600, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReserveStockInput) bool {
			return input.OrderID == orderID && input.UserID == userID && len(input.Items) == 1
		})).
		Return(nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2600, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.MatchedBy(func(input outbound.InitiatePaymentInput) bool {
			return input.OrderID == orderID &&
				input.Amount == 2600 &&
				input.Currency == "USD" &&
				input.IdempotencyKey == orderID.String() &&
				input.PaymentProvider == "default"
		})).
		Return(nil).
		Once()


	behavior := &checkoutSQLBehavior{}
	db := newCheckoutTestDB(t, behavior)
	publisher := &selectiveFailEventPublisher{failEventName: "order.confirmed", err: errors.New("should not be called")}
	txProvider := sqltx.NewProvider(db, func(_ *sql.Tx) TransactionRepos {
		return TransactionRepos{Orders: orders, Saga: saga, Publisher: publisher}
	})

	svc := NewService(orders, saga, snapshots, stock, nil, payment).WithEventing(publisher, txProvider, "order-svc")

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-confirmed-publish-fail"})
	require.NoError(t, err)
	require.Equal(t, outbound.OrderStatusAwaitingPayment, order.Status)

	require.Equal(t, 3, behavior.commitCount)
	require.Equal(t, 0, behavior.rollbackCount)
	require.Len(t, publisher.events, 1)
	require.Equal(t, "order.created", publisher.events[0].EventName)
}

func TestCheckoutPaymentFailureCompensation(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodePaymentDeclined)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)
	publisher := &captureEventPublisher{}

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2100, "SKU-3", "Item 3", 2100), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.MatchedBy(func(input outbound.CreateOrderInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == "idem-payment-fail" &&
				input.Status == outbound.OrderStatusPending
		})).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2100}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock
	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2100, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2100, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2100, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodePaymentDeclined)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodePaymentDeclined)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReserveStockInput) bool {
			return input.OrderID == orderID && input.UserID == userID && len(input.Items) == 1
		})).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.MatchedBy(func(input outbound.InitiatePaymentInput) bool {
			return input.OrderID == orderID &&
				input.Amount == 2100 &&
				input.Currency == "USD" &&
				input.IdempotencyKey == orderID.String()
		})).
		Return(outbound.ErrPaymentDeclined).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReleaseStockInput) bool {
			return input.OrderID == orderID && input.UserID == userID && len(input.Items) == 1
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment).WithEventing(publisher, nil, "order-svc")

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
	require.Equal(t, CheckoutErrorCodePaymentDeclined, CodeOf(err))
	require.Len(t, publisher.events, 2)
	require.Equal(t, "order.created", publisher.events[0].EventName)
	require.Equal(t, "order.cancelled", publisher.events[1].EventName)
	cancelledPayload, ok := publisher.events[1].Payload.(domain.OrderCancelledPayload)
	require.True(t, ok)
	require.Equal(t, string(CheckoutErrorCodePaymentDeclined), cancelledPayload.CancelReasonCode)
}

func TestCheckoutPaymentStageRequestedFailureCompensates(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodeConflict)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-precondition").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2000, "SKU-1", "Item", 2000), nil).
		Once()

	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2000}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock
	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2000, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2000, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2000, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeConflict)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeConflict)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()
	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-precondition"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderSagaStateInvalidTransition)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))

	payment.AssertNotCalled(t, "InitiatePayment", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutStockStageSucceededFailureCompensates(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodeConflict)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-stock-stage-succeeded-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1300, "SKU-1", "Item", 1300), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 1300}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 1300, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1300, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingStock,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeConflict)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()
	saga.EXPECT().
		TransitionStockStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeConflict)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-stock-stage-succeeded-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderSagaStateInvalidTransition)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))
}

func TestCheckoutAwaitingPaymentTransitionFailureCompensates(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodeConflict)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-awaiting-payment-transition-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1400, "SKU-2", "Item", 1400), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 1400}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 1400, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{}, outbound.ErrOrderInvalidStatusTransition).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1400, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingStock,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeConflict)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeConflict)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-awaiting-payment-transition-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrOrderInvalidStatusTransition)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))

	saga.AssertNotCalled(t, "TransitionPaymentStageToRequested", testifymock.Anything, testifymock.Anything)
}

func TestCheckoutPaymentFailureCompensationStockReleaseFailure(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodePaymentDeclined)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-release-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2200, "SKU-4", "Item 4", 2200), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2200}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock
	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2200, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2200, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2200, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodePaymentDeclined)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodePaymentDeclined)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrPaymentDeclined).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrStockReleaseConflict).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-release-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
	require.ErrorIs(t, err, outbound.ErrStockReleaseConflict)
	require.Equal(t, CheckoutErrorCodePaymentDeclined, CodeOf(err))
}

func TestCheckoutPaymentFailureCompensationReleaseAttemptedWhenCancelTransitionFails(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-cancel-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2200, "SKU-4", "Item 4", 2200), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2200}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2200}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2200}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{}, outbound.ErrOrderInvalidStatusTransition).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodePaymentDeclined)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrPaymentDeclined).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-cancel-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
	require.ErrorIs(t, err, outbound.ErrOrderInvalidStatusTransition)
	require.Equal(t, CheckoutErrorCodePaymentDeclined, CodeOf(err))
}

func TestCheckoutPaymentFailureCompensationReleaseAttemptedWhenTransitionPaymentFailedFails(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodePaymentDeclined)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-stage-failed-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2400, "SKU-5", "Item 5", 2400), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2400}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock
	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2400, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2400, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2400, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodePaymentDeclined)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodePaymentDeclined)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested, LastErrorCode: &cancelReason}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrPaymentDeclined).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-stage-failed-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
	require.ErrorIs(t, err, outbound.ErrOrderSagaStateInvalidTransition)
	require.Equal(t, CheckoutErrorCodePaymentDeclined, CodeOf(err))
}

func TestCheckoutPaymentFailureCompensationReleaseAttemptedWhenSetLastErrorCodeFails(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)
	payment := outboundmocks.NewMockCheckoutPaymentService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-payment-set-last-error-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 2500, "SKU-6", "Item 6", 2500), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 2500}, nil).
		Once()

	fromPending := outbound.OrderStatusPending
	fromAwaitingStock := outbound.OrderStatusAwaitingStock
	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 2500, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromPending, outbound.OrderStatusAwaitingStock, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusAwaitingPayment).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 2500, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingStock, outbound.OrderStatusAwaitingPayment, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()
	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2500, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodePaymentDeclined)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionStockStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageRequested}, nil).
		Once()
	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageSucceeded, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodePaymentDeclined)).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	payment.EXPECT().
		InitiatePayment(testifymock.Anything, testifymock.Anything).
		Return(outbound.ErrPaymentDeclined).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.Anything).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, payment)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-payment-set-last-error-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrPaymentDeclined)
	require.ErrorIs(t, err, outbound.ErrOrderSagaStateInvalidTransition)
	require.Equal(t, CheckoutErrorCodePaymentDeclined, CodeOf(err))
}

func TestMapCheckoutCodeStockReleaseNotFound(t *testing.T) {
	t.Parallel()

	require.Equal(t, CheckoutErrorCodeConflict, mapCheckoutCode(outbound.ErrStockReleaseNotFound))
}

func TestCheckoutStockReservationFailureCompensation(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	now := time.Now().UTC()
	cancelReason := string(CheckoutErrorCodeStockUnavailable)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-stock-fail").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1100, "SKU-2", "Item 2", 1100), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.MatchedBy(func(input outbound.CreateOrderInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == "idem-stock-fail" &&
				input.Status == outbound.OrderStatusPending &&
				input.Currency == "USD" &&
				input.TotalAmount == 1100 &&
				len(input.Items) == 1 &&
				input.Items[0].SKU == "SKU-2" &&
				input.Items[0].Quantity == 1
		})).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 1100}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 1100, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			testifymock.MatchedBy(func(status *outbound.OrderStatus) bool {
				return status != nil && *status == outbound.OrderStatusPending
			}),
			outbound.OrderStatusAwaitingStock,
			(*string)(nil),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReserveStockInput) bool {
			return input.OrderID == orderID &&
				input.UserID == userID &&
				len(input.Items) == 1 &&
				input.Items[0].SKU == "SKU-2" &&
				input.Items[0].Quantity == 1
		})).
		Return(outbound.ErrStockReservationUnavailable).
		Once()

	saga.EXPECT().
		TransitionStockStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeStockUnavailable)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted, LastErrorCode: &cancelReason}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1100, CreatedAt: now, UpdatedAt: now}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			testifymock.MatchedBy(func(status *outbound.OrderStatus) bool {
				return status != nil && *status == outbound.OrderStatusAwaitingStock
			}),
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeStockUnavailable)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-stock-fail"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.Equal(t, CheckoutErrorCodeStockUnavailable, CodeOf(err))

	orders.AssertNumberOfCalls(t, "TransitionStatus", 2)
	orders.AssertNumberOfCalls(t, "AppendStatusHistory", 2)
	saga.AssertNumberOfCalls(t, "TransitionStockStageToRequested", 1)
	saga.AssertNumberOfCalls(t, "TransitionStockStageToFailed", 1)
	saga.AssertNumberOfCalls(t, "SetLastErrorCode", 1)
	stock.AssertNumberOfCalls(t, "ReserveStock", 1)
}

func TestCheckoutStockReservationConflictMapsToConflict(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()
	cancelReason := string(CheckoutErrorCodeConflict)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-stock-conflict").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 1100, "SKU-2", "Item 2", 1100), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.MatchedBy(func(input outbound.CreateOrderInput) bool {
			return input.UserID == userID &&
				input.IdempotencyKey == "idem-stock-conflict" &&
				input.Status == outbound.OrderStatusPending &&
				input.Currency == "USD" &&
				input.TotalAmount == 1100 &&
				len(input.Items) == 1 &&
				input.Items[0].SKU == "SKU-2" &&
				input.Items[0].Quantity == 1
		})).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 1100}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingStock, Currency: "USD", TotalAmount: 1100}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			testifymock.MatchedBy(func(status *outbound.OrderStatus) bool {
				return status != nil && *status == outbound.OrderStatusPending
			}),
			outbound.OrderStatusAwaitingStock,
			(*string)(nil),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	saga.EXPECT().
		TransitionStockStageToRequested(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageRequested, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()

	stock.EXPECT().
		ReserveStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReserveStockInput) bool {
			return input.OrderID == orderID &&
				input.UserID == userID &&
				len(input.Items) == 1 &&
				input.Items[0].SKU == "SKU-2" &&
				input.Items[0].Quantity == 1
		})).
		Return(outbound.ErrStockReservationConflict).
		Once()

	saga.EXPECT().
		TransitionStockStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted}, nil).
		Once()
	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, string(CheckoutErrorCodeConflict)).
		Return(outbound.SagaState{OrderID: orderID, StockStage: outbound.SagaStageFailed, PaymentStage: outbound.SagaStageNotStarted, LastErrorCode: &cancelReason}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingStock, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 1100}, nil).
		Once()
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			testifymock.MatchedBy(func(status *outbound.OrderStatus) bool {
				return status != nil && *status == outbound.OrderStatusAwaitingStock
			}),
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == string(CheckoutErrorCodeConflict)
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-stock-conflict"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, outbound.ErrStockReservationConflict)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))

	stock.AssertNumberOfCalls(t, "ReserveStock", 1)
	saga.AssertNumberOfCalls(t, "SetLastErrorCode", 1)
}

func TestCheckoutRepositoryAndTransitionFailuresMapToConflictOrInternal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		createErr     error
		transitionErr error
		expectedCode  CheckoutErrorCode
	}{
		{
			name:         "create maps invalid transition to conflict",
			createErr:    outbound.ErrOrderInvalidStatusTransition,
			expectedCode: CheckoutErrorCodeConflict,
		},
		{
			name:          "transition maps invalid transition to conflict",
			transitionErr: outbound.ErrOrderInvalidStatusTransition,
			expectedCode:  CheckoutErrorCodeConflict,
		},
		{
			name:         "create maps unknown to internal",
			createErr:    errors.New("db down"),
			expectedCode: CheckoutErrorCodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userID := uuid.New()
			orderID := uuid.New()

			orders := outboundmocks.NewMockOrderRepository(t)
			saga := outboundmocks.NewMockOrderSagaStateRepository(t)
			snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
			stock := outboundmocks.NewMockStockReservationService(t)

			orders.EXPECT().
				GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-map").
				Return(outbound.Order{}, outbound.ErrOrderNotFound).
				Once()
			snapshots.EXPECT().
				GetCheckoutSnapshot(testifymock.Anything, userID).
				Return(checkoutSnapshotWithSingleItem(userID, 500, "SKU-1", "Item", 500), nil).
				Once()

			if tt.createErr != nil {
				orders.EXPECT().
					CreateWithItems(testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{}, tt.createErr).
					Once()
			} else {
				orders.EXPECT().
					CreateWithItems(testifymock.Anything, testifymock.Anything).
					Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD", TotalAmount: 500}, nil).
					Once()
				orders.EXPECT().
					TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusPending, outbound.OrderStatusAwaitingStock).
					Return(outbound.Order{}, tt.transitionErr).
					Once()
			}

			svc := NewService(orders, saga, snapshots, stock, nil, nil)

			order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-map"})
			require.Equal(t, outbound.Order{}, order)
			require.Error(t, err)
			require.Equal(t, tt.expectedCode, CodeOf(err))
			if tt.transitionErr != nil {
				saga.AssertNotCalled(t, "TransitionStockStageToRequested", testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

func TestCheckoutUsesDomainTransitionValidation(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByUserIDAndIdempotencyKey(testifymock.Anything, userID, "idem-transition").
		Return(outbound.Order{}, outbound.ErrOrderNotFound).
		Once()
	snapshots.EXPECT().
		GetCheckoutSnapshot(testifymock.Anything, userID).
		Return(checkoutSnapshotWithSingleItem(userID, 300, "SKU-1", "Item", 300), nil).
		Once()
	orders.EXPECT().
		CreateWithItems(testifymock.Anything, testifymock.Anything).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusConfirmed, Currency: "USD", TotalAmount: 300}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil)

	order, err := svc.Checkout(context.Background(), CheckoutInput{UserID: userID, IdempotencyKey: "idem-transition"})
	require.Equal(t, outbound.Order{}, order)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrConfirmedOrderImmutable)
	require.Equal(t, CheckoutErrorCodeConflict, CodeOf(err))

	orders.AssertNotCalled(t, "TransitionStatus", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
}

func TestGetOrder(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()

	tests := []struct {
		name          string
		input         GetOrderInput
		setupOrderGet func(repo *outboundmocks.MockOrderRepository)
		expectedCode  CheckoutErrorCode
		expectedErrIs error
		expectedOrder outbound.Order
	}{
		{
			name:         "invalid input missing user id",
			input:        GetOrderInput{OrderID: orderID},
			expectedCode: CheckoutErrorCodeInvalidArgument,
		},
		{
			name:         "invalid input missing order id",
			input:        GetOrderInput{UserID: userID},
			expectedCode: CheckoutErrorCodeInvalidArgument,
		},
		{
			name:  "order not found",
			input: GetOrderInput{UserID: userID, OrderID: orderID},
			setupOrderGet: func(repo *outboundmocks.MockOrderRepository) {
				repo.EXPECT().
					GetByID(testifymock.Anything, orderID).
					Return(outbound.Order{}, outbound.ErrOrderNotFound).
					Once()
			},
			expectedCode:  CheckoutErrorCodeCartNotFound,
			expectedErrIs: outbound.ErrOrderNotFound,
		},
		{
			name:  "ownership mismatch mapped as not found",
			input: GetOrderInput{UserID: userID, OrderID: orderID},
			setupOrderGet: func(repo *outboundmocks.MockOrderRepository) {
				repo.EXPECT().
					GetByID(testifymock.Anything, orderID).
					Return(outbound.Order{OrderID: orderID, UserID: uuid.New(), Status: outbound.OrderStatusPending}, nil).
					Once()
			},
			expectedCode:  CheckoutErrorCodeCartNotFound,
			expectedErrIs: outbound.ErrOrderNotFound,
		},
		{
			name:  "repository internal error",
			input: GetOrderInput{UserID: userID, OrderID: orderID},
			setupOrderGet: func(repo *outboundmocks.MockOrderRepository) {
				repo.EXPECT().
					GetByID(testifymock.Anything, orderID).
					Return(outbound.Order{}, errors.New("db down")).
					Once()
			},
			expectedCode: CheckoutErrorCodeInternal,
		},
		{
			name:  "success",
			input: GetOrderInput{UserID: userID, OrderID: orderID},
			setupOrderGet: func(repo *outboundmocks.MockOrderRepository) {
				repo.EXPECT().
					GetByID(testifymock.Anything, orderID).
					Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 1000}, nil).
					Once()
			},
			expectedOrder: outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusAwaitingPayment, Currency: "USD", TotalAmount: 1000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := outboundmocks.NewMockOrderRepository(t)
			saga := outboundmocks.NewMockOrderSagaStateRepository(t)
			snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
			stock := outboundmocks.NewMockStockReservationService(t)

			if tt.setupOrderGet != nil {
				tt.setupOrderGet(orders)
			}

			svc := NewService(orders, saga, snapshots, stock, nil, nil)

			order, err := svc.GetOrder(context.Background(), tt.input)
			if tt.expectedCode == "" {
				require.NoError(t, err)
				require.Equal(t, tt.expectedOrder, order)
				return
			}

			require.Equal(t, outbound.Order{}, order)
			require.Error(t, err)
			require.Equal(t, tt.expectedCode, CodeOf(err))
			if tt.expectedErrIs != nil {
				require.ErrorIs(t, err, tt.expectedErrIs)
			}

			if tt.setupOrderGet == nil {
				orders.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
			}
		})
	}
}

func checkoutSnapshotWithSingleItem(userID uuid.UUID, totalAmount int64, sku string, name string, unitPrice int64) outbound.CheckoutSnapshot {
	return outbound.CheckoutSnapshot{
		UserID:      userID,
		Currency:    "USD",
		TotalAmount: totalAmount,
		Items: []outbound.CheckoutSnapshotItem{{
			ProductID: uuid.New(),
			SKU:       sku,
			Name:      name,
			Quantity:  1,
			UnitPrice: unitPrice,
			LineTotal: totalAmount,
			Currency:  "USD",
		}},
	}
}

type concurrentReplayOrderRepo struct {
	userID         uuid.UUID
	idempotencyKey string
	fingerprint    string
	orderID        uuid.UUID

	mu      sync.Mutex
	created bool

	createCalls atomic.Int32
}

func newConcurrentReplayOrderRepo(userID uuid.UUID, idempotencyKey string, paymentMethod string) *concurrentReplayOrderRepo {
	return &concurrentReplayOrderRepo{
		userID:         userID,
		idempotencyKey: idempotencyKey,
		fingerprint:    checkoutPayloadFingerprint(outbound.CheckoutIdempotencyPayload{PaymentMethod: paymentMethod}),
		orderID:        uuid.New(),
	}
}

func (r *concurrentReplayOrderRepo) createCallCount() int {
	return int(r.createCalls.Load())
}

func (r *concurrentReplayOrderRepo) CreateWithItems(context.Context, outbound.CreateOrderInput) (outbound.Order, error) {
	r.createCalls.Add(1)

	r.mu.Lock()
	r.created = true
	r.mu.Unlock()

	return outbound.Order{}, outbound.ErrOrderIdempotencyConflict
}

func (r *concurrentReplayOrderRepo) GetByID(context.Context, uuid.UUID) (outbound.Order, error) {
	return outbound.Order{}, outbound.ErrOrderNotFound
}

func (r *concurrentReplayOrderRepo) GetByUserIDAndIdempotencyKey(_ context.Context, userID uuid.UUID, idempotencyKey string) (outbound.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.created || userID != r.userID || idempotencyKey != r.idempotencyKey {
		return outbound.Order{}, outbound.ErrOrderNotFound
	}

	return outbound.Order{
		OrderID:     r.orderID,
		UserID:      r.userID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 1000,
	}, nil
}

func (r *concurrentReplayOrderRepo) TransitionStatus(context.Context, uuid.UUID, outbound.OrderStatus, outbound.OrderStatus) (outbound.Order, error) {
	return outbound.Order{}, outbound.ErrOrderInvalidStatusTransition
}

func (r *concurrentReplayOrderRepo) AppendStatusHistory(context.Context, uuid.UUID, *outbound.OrderStatus, outbound.OrderStatus, *string) (outbound.OrderStatusHistory, error) {
	return outbound.OrderStatusHistory{}, errors.New("not implemented")
}

func (r *concurrentReplayOrderRepo) GetPayloadFingerprint(_ context.Context, userID uuid.UUID, idempotencyKey string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.created || userID != r.userID || idempotencyKey != r.idempotencyKey {
		return "", outbound.ErrOrderNotFound
	}

	return r.fingerprint, nil
}

type captureEventPublisher struct {
	events []domain.DomainEvent
	err    error
}

func (p *captureEventPublisher) Publish(_ context.Context, event domain.DomainEvent) error {
	p.events = append(p.events, event)
	return p.err
}

type selectiveFailEventPublisher struct {
	events         []domain.DomainEvent
	failEventName  string
	err            error
}

func (p *selectiveFailEventPublisher) Publish(_ context.Context, event domain.DomainEvent) error {
	p.events = append(p.events, event)
	if event.EventName == p.failEventName {
		return p.err
	}

	return nil
}

func TestCheckoutCreateOrderAndOutboxAtomicWithTxProvider(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orderID := uuid.New()

	tests := []struct {
		name            string
		publishErr      error
		wantOrder       outbound.Order
		wantErrContains string
		wantCommits     int
		wantRollbacks   int
	}{
		{
			name:            "publish failure rolls back order creation",
			publishErr:      errors.New("append outbox record failed"),
			wantOrder:       outbound.Order{},
			wantErrContains: "publish order created event",
			wantCommits:     0,
			wantRollbacks:   1,
		},
		{
			name:          "publish success commits both",
			wantOrder:     outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusPending, Currency: "USD"},
			wantCommits:   1,
			wantRollbacks: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := outboundmocks.NewMockOrderRepository(t)
			saga := outboundmocks.NewMockOrderSagaStateRepository(t)
			snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
			stock := outboundmocks.NewMockStockReservationService(t)

			snapshot := checkoutSnapshotWithSingleItem(userID, 1500, "SKU-1", "Item", 1500)

			baseOrder := outbound.Order{
				OrderID:     orderID,
				UserID:      userID,
				Status:      outbound.OrderStatusPending,
				Currency:    "USD",
				TotalAmount: 1500,
				Items: []outbound.OrderItem{{
					OrderID:   orderID,
					ProductID: snapshot.Items[0].ProductID,
					SKU:       snapshot.Items[0].SKU,
					Name:      snapshot.Items[0].Name,
					Quantity:  snapshot.Items[0].Quantity,
					UnitPrice: snapshot.Items[0].UnitPrice,
					LineTotal: snapshot.Items[0].LineTotal,
					Currency:  snapshot.Items[0].Currency,
				}},
			}

			orders.EXPECT().CreateWithItems(testifymock.Anything, testifymock.Anything).Return(baseOrder, nil).Once()

			behavior := &checkoutSQLBehavior{}
			db := newCheckoutTestDB(t, behavior)

			publisher := &captureEventPublisher{err: tt.publishErr}
			txProvider := sqltx.NewProvider(db, func(_ *sql.Tx) TransactionRepos {
				return TransactionRepos{Orders: orders, Saga: saga, Publisher: publisher}
			})

			svc := NewService(orders, saga, snapshots, stock, nil, nil).WithEventing(publisher, txProvider, "order-svc")

			order, err := svc.createOrderAndPublishCreatedEvent(context.Background(), userID, "idem-tx", "", snapshot)
			if tt.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErrContains)
				require.Equal(t, tt.wantOrder, order)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantOrder.Status, order.Status)
				require.Equal(t, tt.wantOrder.OrderID, order.OrderID)
			}

			require.Equal(t, tt.wantCommits, behavior.commitCount)
			require.Equal(t, tt.wantRollbacks, behavior.rollbackCount)
		})
	}
}

var checkoutTestDriverSeq uint64

type checkoutSQLBehavior struct {
	mu sync.Mutex

	beginErr    error
	commitErr   error
	rollbackErr error

	commitCount   int
	rollbackCount int
}

func newCheckoutTestDB(t *testing.T, behavior *checkoutSQLBehavior) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("checkout_tx_test_driver_%d", atomic.AddUint64(&checkoutTestDriverSeq, 1))
	sql.Register(name, &checkoutTestDriver{behavior: behavior})

	db, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

type checkoutTestDriver struct {
	behavior *checkoutSQLBehavior
}

func (d *checkoutTestDriver) Open(_ string) (driver.Conn, error) {
	return &checkoutTestConn{behavior: d.behavior}, nil
}

type checkoutTestConn struct {
	behavior *checkoutSQLBehavior
}

func (c *checkoutTestConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

func (c *checkoutTestConn) Close() error {
	return nil
}

func (c *checkoutTestConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *checkoutTestConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.behavior.mu.Lock()
	defer c.behavior.mu.Unlock()

	if c.behavior.beginErr != nil {
		return nil, c.behavior.beginErr
	}

	return &checkoutTestTx{behavior: c.behavior}, nil
}

type checkoutTestTx struct {
	behavior *checkoutSQLBehavior
}

func (tx *checkoutTestTx) Commit() error {
	tx.behavior.mu.Lock()
	defer tx.behavior.mu.Unlock()

	tx.behavior.commitCount++
	return tx.behavior.commitErr
}

func (tx *checkoutTestTx) Rollback() error {
	tx.behavior.mu.Lock()
	defer tx.behavior.mu.Unlock()

	tx.behavior.rollbackCount++
	return tx.behavior.rollbackErr
}
