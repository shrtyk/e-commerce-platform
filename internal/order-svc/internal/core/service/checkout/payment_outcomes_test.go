package checkout

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound/mocks"
)

func TestHandlePaymentSucceededTransitionsOrderToConfirmed(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()
	createdAt := time.Now().UTC().Add(-time.Minute)

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	order := outbound.Order{
		OrderID:     orderID,
		UserID:      userID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 4200,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(order, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageSucceeded}, nil).
		Once()

	saga.EXPECT().
		ClearLastErrorCode(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageSucceeded}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusConfirmed).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusConfirmed, Currency: "USD", TotalAmount: 4200, CreatedAt: createdAt, UpdatedAt: createdAt}, nil).
		Once()

	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment
	orders.EXPECT().
		AppendStatusHistory(testifymock.Anything, orderID, &fromAwaitingPayment, outbound.OrderStatusConfirmed, (*string)(nil)).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentSucceeded(context.Background(), HandlePaymentSucceededInput{OrderID: orderID})
	require.NoError(t, err)
}

func TestHandlePaymentSucceededReplayIsNoOp(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusConfirmed}, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToSucceeded(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()

	saga.EXPECT().
		ClearLastErrorCode(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageSucceeded}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, nil, nil)
	err := svc.HandlePaymentSucceeded(context.Background(), HandlePaymentSucceededInput{OrderID: orderID})
	require.NoError(t, err)
}

func TestHandlePaymentFailedCancelsOrderAndReleasesStock(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()
	productID := uuid.New()
	failureCode := "gateway_declined"

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	order := outbound.Order{
		OrderID:     orderID,
		UserID:      userID,
		Status:      outbound.OrderStatusAwaitingPayment,
		Currency:    "USD",
		TotalAmount: 2100,
		Items: []outbound.OrderItem{{
			ProductID: productID,
			SKU:       "SKU-1",
			Quantity:  2,
		}},
	}

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(order, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()

	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, failureCode).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusCancelled, Currency: "USD", TotalAmount: 2100}, nil).
		Once()

	fromAwaitingPayment := outbound.OrderStatusAwaitingPayment
	orders.EXPECT().
		AppendStatusHistory(
			testifymock.Anything,
			orderID,
			&fromAwaitingPayment,
			outbound.OrderStatusCancelled,
			testifymock.MatchedBy(func(reason *string) bool {
				return reason != nil && *reason == failureCode
			}),
		).
		Return(outbound.OrderStatusHistory{}, nil).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReleaseStockInput) bool {
			return input.OrderID == orderID &&
				input.UserID == userID &&
				len(input.Items) == 1 &&
				input.Items[0].ProductID == productID &&
				input.Items[0].SKU == "SKU-1" &&
				input.Items[0].Quantity == 2
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID, FailureCode: failureCode})
	require.NoError(t, err)
}

func TestHandlePaymentFailedReplayIsNoOp(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()
	productID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(outbound.Order{
			OrderID: orderID,
			UserID:  userID,
			Status:  outbound.OrderStatusCancelled,
			Items: []outbound.OrderItem{{
				ProductID: productID,
				SKU:       "SKU-1",
				Quantity:  1,
			}},
		}, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()

	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, "payment_declined").
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()

	stockRelease.EXPECT().
		ReleaseStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReleaseStockInput) bool {
			return input.OrderID == orderID &&
				input.UserID == userID &&
				len(input.Items) == 1 &&
				input.Items[0].ProductID == productID &&
				input.Items[0].SKU == "SKU-1" &&
				input.Items[0].Quantity == 1
		})).
		Return(nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID})
	require.NoError(t, err)
}

func TestHandlePaymentFailedConfirmedOrderIsNoOp(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusConfirmed}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID, FailureCode: "late_failed_event"})
	require.NoError(t, err)

	saga.AssertNotCalled(t, "TransitionPaymentStageToFailed", testifymock.Anything, testifymock.Anything)
	saga.AssertNotCalled(t, "SetLastErrorCode", testifymock.Anything, testifymock.Anything, testifymock.Anything)
	stockRelease.AssertNotCalled(t, "ReleaseStock", testifymock.Anything, testifymock.Anything)
}

func TestHandlePaymentFailedCancelConflictWithConfirmedOrderIsNoOp(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()
	productID := uuid.New()
	failureCode := "gateway_declined"

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(outbound.Order{
			OrderID: orderID,
			UserID:  userID,
			Status:  outbound.OrderStatusAwaitingPayment,
			Items: []outbound.OrderItem{{
				ProductID: productID,
				SKU:       "SKU-1",
				Quantity:  1,
			}},
		}, nil).
		Once()

	saga.EXPECT().
		TransitionPaymentStageToFailed(testifymock.Anything, orderID).
		Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
		Once()

	saga.EXPECT().
		SetLastErrorCode(testifymock.Anything, orderID, failureCode).
		Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageFailed}, nil).
		Once()

	orders.EXPECT().
		TransitionStatus(testifymock.Anything, orderID, outbound.OrderStatusAwaitingPayment, outbound.OrderStatusCancelled).
		Return(outbound.Order{}, outbound.ErrOrderInvalidStatusTransition).
		Once()

	orders.EXPECT().
		GetByID(testifymock.Anything, orderID).
		Return(outbound.Order{OrderID: orderID, UserID: userID, Status: outbound.OrderStatusConfirmed}, nil).
		Once()

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID, FailureCode: failureCode})
	require.NoError(t, err)

	stockRelease.AssertNotCalled(t, "ReleaseStock", testifymock.Anything, testifymock.Anything)
}

func TestHandlePaymentFailedCancelledOrderRetriesStockReleaseOnDuplicateEvents(t *testing.T) {
	t.Parallel()

	orderID := uuid.New()
	userID := uuid.New()
	productID := uuid.New()

	orders := outboundmocks.NewMockOrderRepository(t)
	saga := outboundmocks.NewMockOrderSagaStateRepository(t)
	snapshots := outboundmocks.NewMockCheckoutSnapshotRepository(t)
	stock := outboundmocks.NewMockStockReservationService(t)
	stockRelease := outboundmocks.NewMockStockReleaseService(t)

	for range 2 {
		orders.EXPECT().
			GetByID(testifymock.Anything, orderID).
			Return(outbound.Order{
				OrderID: orderID,
				UserID:  userID,
				Status:  outbound.OrderStatusCancelled,
				Items: []outbound.OrderItem{{
					ProductID: productID,
					SKU:       "SKU-1",
					Quantity:  1,
				}},
			}, nil).
			Once()

		saga.EXPECT().
			TransitionPaymentStageToFailed(testifymock.Anything, orderID).
			Return(outbound.SagaState{}, outbound.ErrOrderSagaStateInvalidTransition).
			Once()

		saga.EXPECT().
			SetLastErrorCode(testifymock.Anything, orderID, "payment_declined").
			Return(outbound.SagaState{OrderID: orderID, PaymentStage: outbound.SagaStageFailed}, nil).
			Once()

		stockRelease.EXPECT().
			ReleaseStock(testifymock.Anything, testifymock.MatchedBy(func(input outbound.ReleaseStockInput) bool {
				return input.OrderID == orderID &&
					input.UserID == userID &&
					len(input.Items) == 1 &&
					input.Items[0].ProductID == productID
			})).
			Return(nil).
			Once()
	}

	svc := NewService(orders, saga, snapshots, stock, stockRelease, nil)
	err := svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID})
	require.NoError(t, err)
	err = svc.HandlePaymentFailed(context.Background(), HandlePaymentFailedInput{OrderID: orderID})
	require.NoError(t, err)
}
