package checkout

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

const maxIdempotencyKeyLength = 255

type Service struct {
	orders           outbound.OrderRepository
	saga             outbound.OrderSagaStateRepository
	snapshots        outbound.CheckoutSnapshotRepository
	stock            outbound.StockReservationService
	stockRelease     outbound.StockReleaseService
	payment          outbound.CheckoutPaymentService
	idempotencyGuard outbound.CheckoutIdempotencyGuard
}

type CheckoutInput struct {
	UserID         uuid.UUID
	IdempotencyKey string
	PaymentMethod  *string
}

func NewService(
	orders outbound.OrderRepository,
	saga outbound.OrderSagaStateRepository,
	snapshots outbound.CheckoutSnapshotRepository,
	stock outbound.StockReservationService,
	stockRelease outbound.StockReleaseService,
	payment outbound.CheckoutPaymentService,
	guards ...outbound.CheckoutIdempotencyGuard,
) *Service {
	release := outbound.StockReleaseService(noopStockReleaseService{})
	if stockRelease != nil {
		release = stockRelease
	}

	paymentService := outbound.CheckoutPaymentService(noopCheckoutPaymentService{})
	if payment != nil {
		paymentService = payment
	}

	guard := outbound.CheckoutIdempotencyGuard(noopCheckoutIdempotencyGuard{})
	if len(guards) > 0 && guards[0] != nil {
		guard = guards[0]
	}

	return &Service{
		orders:           orders,
		saga:             saga,
		snapshots:        snapshots,
		stock:            stock,
		stockRelease:     release,
		payment:          paymentService,
		idempotencyGuard: guard,
	}
}

func (s *Service) Checkout(ctx context.Context, input CheckoutInput) (outbound.Order, error) {
	if err := s.validateInput(input); err != nil {
		return outbound.Order{}, err
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)

	existing, err := s.orders.GetByUserIDAndIdempotencyKey(ctx, input.UserID, idempotencyKey)
	if err == nil {
		if guardErr := s.idempotencyGuard.ValidateCheckoutIdempotency(
			ctx,
			outbound.ValidateCheckoutIdempotencyInput{
				UserID:         input.UserID,
				IdempotencyKey: idempotencyKey,
				Payload: outbound.CheckoutIdempotencyPayload{
					PaymentMethod: normalizePaymentMethod(input.PaymentMethod),
				},
			},
		); guardErr != nil {
			return outbound.Order{}, wrapCode(mapCheckoutCode(guardErr), "validate checkout idempotency payload", guardErr)
		}

		return existing, nil
	}
	if !errors.Is(err, outbound.ErrOrderNotFound) {
		return outbound.Order{}, wrapCode(
			CheckoutErrorCodeInternal,
			"get order by user id and idempotency key",
			err,
		)
	}

	snapshot, err := s.snapshots.GetCheckoutSnapshot(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, outbound.ErrCheckoutSnapshotNotFound) {
			return outbound.Order{}, wrapCode(CheckoutErrorCodeCartNotFound, "get checkout snapshot", err)
		}

		return outbound.Order{}, wrapCode(CheckoutErrorCodeInternal, "get checkout snapshot", err)
	}

	if len(snapshot.Items) == 0 {
		return outbound.Order{}, newCodeError(CheckoutErrorCodeCartEmpty, "checkout snapshot has no items")
	}

	createdOrder, err := s.orders.CreateWithItems(ctx, toCreateOrderInput(input.UserID, idempotencyKey, snapshot))
	if err != nil {
		return outbound.Order{}, wrapCode(mapCheckoutCode(err), "create order with items", err)
	}

	order, err := s.transitionOrderStatus(ctx, createdOrder, domain.OrderStatusAwaitingStock, nil)
	if err != nil {
		return outbound.Order{}, err
	}

	if _, err := s.transitionStockStage(ctx, createdOrder.OrderID, domain.SagaStageRequested); err != nil {
		return outbound.Order{}, err
	}

	reserveStockInput := toReserveStockInput(createdOrder.OrderID, input.UserID, snapshot)
	if err := s.stock.ReserveStock(ctx, reserveStockInput); err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensateStockFailure(ctx, createdOrder.OrderID, order.Status, code); compErr != nil {
			return outbound.Order{}, compErr
		}

		return outbound.Order{}, wrapCode(code, "reserve stock", err)
	}

	if _, err := s.transitionStockStage(ctx, createdOrder.OrderID, domain.SagaStageSucceeded); err != nil {
		return outbound.Order{}, err
	}

	order, err = s.transitionOrderStatus(ctx, order, domain.OrderStatusAwaitingPayment, nil)
	if err != nil {
		return outbound.Order{}, err
	}

	if _, err := s.transitionPaymentStage(ctx, createdOrder.OrderID, domain.SagaStageRequested); err != nil {
		return outbound.Order{}, err
	}

	if err := s.payment.InitiatePayment(ctx, toInitiatePaymentInput(order)); err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensatePaymentFailure(ctx, createdOrder.OrderID, order.Status, reserveStockInput, code); compErr != nil {
			return outbound.Order{}, &CheckoutError{
				Code: code,
				Err:  fmt.Errorf("initiate payment: %w", errors.Join(err, compErr)),
			}
		}

		return outbound.Order{}, wrapCode(code, "initiate payment", err)
	}

	if _, err := s.transitionPaymentStage(ctx, createdOrder.OrderID, domain.SagaStageSucceeded); err != nil {
		return outbound.Order{}, err
	}

	return order, nil
}

func (s *Service) compensateStockFailure(
	ctx context.Context,
	orderID uuid.UUID,
	currentStatus outbound.OrderStatus,
	code CheckoutErrorCode,
) error {
	if _, err := s.transitionStockStage(ctx, orderID, domain.SagaStageFailed); err != nil {
		return err
	}

	if _, err := s.saga.SetLastErrorCode(ctx, orderID, string(code)); err != nil {
		return wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
	}

	order := outbound.Order{OrderID: orderID, Status: currentStatus}
	reason := string(code)
	_, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusCancelled, &reason)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) compensatePaymentFailure(
	ctx context.Context,
	orderID uuid.UUID,
	currentStatus outbound.OrderStatus,
	releaseStockInput outbound.ReserveStockInput,
	code CheckoutErrorCode,
) error {
	if _, err := s.transitionPaymentStage(ctx, orderID, domain.SagaStageFailed); err != nil {
		return err
	}

	if _, err := s.saga.SetLastErrorCode(ctx, orderID, string(code)); err != nil {
		return wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
	}

	var compensationErr error

	order := outbound.Order{OrderID: orderID, Status: currentStatus}
	reason := string(code)
	if _, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusCancelled, &reason); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("cancel order after payment failure: %w", err))
	}

	if err := s.stockRelease.ReleaseStock(ctx, toReleaseStockInput(releaseStockInput)); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("release stock after payment failure: %w", err))
	}

	if compensationErr != nil {
		return compensationErr
	}

	return nil
}

func (s *Service) transitionOrderStatus(
	ctx context.Context,
	order outbound.Order,
	target domain.OrderStatus,
	reasonCode *string,
) (outbound.Order, error) {
	fromStatus := toDomainOrderStatus(order.Status)
	next, err := domain.TransitionOrderStatus(fromStatus, target)
	if err != nil {
		return outbound.Order{}, wrapCode(CheckoutErrorCodeConflict, "validate order transition", err)
	}

	updated, err := s.orders.TransitionStatus(
		ctx,
		order.OrderID,
		toOutboundOrderStatus(fromStatus),
		toOutboundOrderStatus(next),
	)
	if err != nil {
		return outbound.Order{}, wrapCode(mapCheckoutCode(err), "transition order status", err)
	}

	fromStatusOutbound := toOutboundOrderStatus(fromStatus)
	if _, err := s.orders.AppendStatusHistory(ctx, order.OrderID, &fromStatusOutbound, updated.Status, reasonCode); err != nil {
		return outbound.Order{}, wrapCode(mapCheckoutCode(err), "append order status history", err)
	}

	return updated, nil
}

func (s *Service) transitionStockStage(
	ctx context.Context,
	orderID uuid.UUID,
	target domain.SagaStage,
) (outbound.SagaState, error) {
	from := domain.SagaStageNotStarted
	switch target {
	case domain.SagaStageRequested:
		from = domain.SagaStageNotStarted
	case domain.SagaStageSucceeded, domain.SagaStageFailed:
		from = domain.SagaStageRequested
	}

	next, err := domain.TransitionStockStage(from, target)
	if err != nil {
		return outbound.SagaState{}, wrapCode(CheckoutErrorCodeConflict, "validate stock stage transition", err)
	}

	switch next {
	case domain.SagaStageRequested:
		state, reqErr := s.saga.TransitionStockStageToRequested(ctx, orderID)
		if reqErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(reqErr), "transition stock stage to requested", reqErr)
		}

		return state, nil
	case domain.SagaStageSucceeded:
		state, sucErr := s.saga.TransitionStockStageToSucceeded(ctx, orderID)
		if sucErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(sucErr), "transition stock stage to succeeded", sucErr)
		}

		return state, nil
	case domain.SagaStageFailed:
		state, failErr := s.saga.TransitionStockStageToFailed(ctx, orderID)
		if failErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(failErr), "transition stock stage to failed", failErr)
		}

		return state, nil
	default:
		return outbound.SagaState{}, newCodeError(CheckoutErrorCodeInternal, "unsupported stock stage")
	}
}

func (s *Service) transitionPaymentStage(
	ctx context.Context,
	orderID uuid.UUID,
	target domain.SagaStage,
) (outbound.SagaState, error) {
	from := domain.SagaStageNotStarted
	switch target {
	case domain.SagaStageRequested:
		from = domain.SagaStageNotStarted
	case domain.SagaStageSucceeded, domain.SagaStageFailed:
		from = domain.SagaStageRequested
	}

	next, err := domain.TransitionPaymentStage(domain.SagaStageSucceeded, from, target)
	if err != nil {
		return outbound.SagaState{}, wrapCode(CheckoutErrorCodeConflict, "validate payment stage transition", err)
	}

	switch next {
	case domain.SagaStageRequested:
		state, reqErr := s.saga.TransitionPaymentStageToRequested(ctx, orderID)
		if reqErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(reqErr), "transition payment stage to requested", reqErr)
		}

		return state, nil
	case domain.SagaStageSucceeded:
		state, sucErr := s.saga.TransitionPaymentStageToSucceeded(ctx, orderID)
		if sucErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(sucErr), "transition payment stage to succeeded", sucErr)
		}

		return state, nil
	case domain.SagaStageFailed:
		state, failErr := s.saga.TransitionPaymentStageToFailed(ctx, orderID)
		if failErr != nil {
			return outbound.SagaState{}, wrapCode(mapCheckoutCode(failErr), "transition payment stage to failed", failErr)
		}

		return state, nil
	default:
		return outbound.SagaState{}, newCodeError(CheckoutErrorCodeInternal, "unsupported payment stage")
	}
}

func (s *Service) validateInput(input CheckoutInput) error {
	if input.UserID == uuid.Nil {
		return newCodeError(CheckoutErrorCodeInvalidArgument, "checkout input user_id is required")
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		return newCodeError(CheckoutErrorCodeInvalidArgument, "checkout input idempotency key is required")
	}

	if len(idempotencyKey) > maxIdempotencyKeyLength {
		return newCodeError(CheckoutErrorCodeInvalidArgument, "checkout input idempotency key is too long")
	}

	return nil
}

func toCreateOrderInput(userID uuid.UUID, idempotencyKey string, snapshot outbound.CheckoutSnapshot) outbound.CreateOrderInput {
	items := make([]outbound.CreateOrderItemInput, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, outbound.CreateOrderItemInput{
			ProductID: item.ProductID,
			SKU:       item.SKU,
			Name:      item.Name,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice,
			LineTotal: item.LineTotal,
			Currency:  item.Currency,
		})
	}

	return outbound.CreateOrderInput{
		OrderID:        uuid.New(),
		UserID:         userID,
		Status:         outbound.OrderStatusPending,
		Currency:       snapshot.Currency,
		TotalAmount:    snapshot.TotalAmount,
		IdempotencyKey: idempotencyKey,
		Items:          items,
	}
}

func toReserveStockInput(orderID uuid.UUID, userID uuid.UUID, snapshot outbound.CheckoutSnapshot) outbound.ReserveStockInput {
	items := make([]outbound.ReserveStockItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, outbound.ReserveStockItem{
			ProductID: item.ProductID,
			SKU:       item.SKU,
			Quantity:  item.Quantity,
		})
	}

	return outbound.ReserveStockInput{
		OrderID: orderID,
		UserID:  userID,
		Items:   items,
	}
}

func toReleaseStockInput(input outbound.ReserveStockInput) outbound.ReleaseStockInput {
	items := make([]outbound.ReleaseStockItem, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, outbound.ReleaseStockItem{
			ProductID: item.ProductID,
			SKU:       item.SKU,
			Quantity:  item.Quantity,
		})
	}

	return outbound.ReleaseStockInput{
		OrderID: input.OrderID,
		UserID:  input.UserID,
		Items:   items,
	}
}

func toInitiatePaymentInput(order outbound.Order) outbound.InitiatePaymentInput {
	return outbound.InitiatePaymentInput{
		OrderID:         order.OrderID,
		Amount:          order.TotalAmount,
		Currency:        order.Currency,
		IdempotencyKey:  order.OrderID.String(),
		PaymentProvider: "default",
	}
}

func mapCheckoutCode(err error) CheckoutErrorCode {
	switch {
	case errors.Is(err, outbound.ErrCheckoutSnapshotNotFound):
		return CheckoutErrorCodeCartNotFound
	case errors.Is(err, outbound.ErrCheckoutIdempotencyPayloadMismatch),
		errors.Is(err, outbound.ErrOrderIdempotencyConflict):
		return CheckoutErrorCodeWrongIdempotencyKeyPayload
	case errors.Is(err, outbound.ErrStockReservationSKUNotFound):
		return CheckoutErrorCodeSKUNotFound
	case errors.Is(err, outbound.ErrStockReservationUnavailable):
		return CheckoutErrorCodeStockUnavailable
	case errors.Is(err, outbound.ErrStockReservationConflict):
		return CheckoutErrorCodeConflict
	case errors.Is(err, outbound.ErrStockReleaseNotFound),
		errors.Is(err, outbound.ErrStockReleaseUnavailable),
		errors.Is(err, outbound.ErrStockReleaseConflict):
		return CheckoutErrorCodeConflict
	case errors.Is(err, outbound.ErrPaymentDeclined):
		return CheckoutErrorCodePaymentDeclined
	case errors.Is(err, outbound.ErrPaymentConflict):
		return CheckoutErrorCodeConflict
	case errors.Is(err, outbound.ErrOrderAlreadyExists),
		errors.Is(err, outbound.ErrOrderInvalidStatusTransition),
		errors.Is(err, outbound.ErrOrderSagaStateInvalidTransition),
		errors.Is(err, domain.ErrInvalidOrderStatusTransition),
		errors.Is(err, domain.ErrCancelledOrderTerminal),
		errors.Is(err, domain.ErrConfirmedOrderImmutable),
		errors.Is(err, domain.ErrInvalidSagaStageTransition),
		errors.Is(err, domain.ErrSagaTerminalStateConflict):
		return CheckoutErrorCodeConflict
	default:
		return CheckoutErrorCodeInternal
	}
}

func wrapCode(code CheckoutErrorCode, operation string, err error) error {
	if err == nil {
		return nil
	}

	e, ok := errors.AsType[*CheckoutError](err)
	if ok {
		if operation == "" {
			return e
		}

		return &CheckoutError{Code: e.Code, Err: fmt.Errorf("%s: %w", operation, e.Err)}
	}

	if operation == "" {
		return &CheckoutError{Code: code, Err: err}
	}

	return &CheckoutError{Code: code, Err: fmt.Errorf("%s: %w", operation, err)}
}

func toDomainOrderStatus(status outbound.OrderStatus) domain.OrderStatus {
	return domain.OrderStatus(status)
}

func toOutboundOrderStatus(status domain.OrderStatus) outbound.OrderStatus {
	return outbound.OrderStatus(status)
}

func normalizePaymentMethod(paymentMethod *string) string {
	if paymentMethod == nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(*paymentMethod))
}

type noopCheckoutIdempotencyGuard struct{}

func (noopCheckoutIdempotencyGuard) ValidateCheckoutIdempotency(context.Context, outbound.ValidateCheckoutIdempotencyInput) error {
	return nil
}

type noopStockReleaseService struct{}

func (noopStockReleaseService) ReleaseStock(context.Context, outbound.ReleaseStockInput) error {
	return nil
}

type noopCheckoutPaymentService struct{}

func (noopCheckoutPaymentService) InitiatePayment(context.Context, outbound.InitiatePaymentInput) error {
	return nil
}
