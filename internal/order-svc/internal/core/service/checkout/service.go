package checkout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx"

	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/order-svc/internal/core/ports/outbound"
)

const maxIdempotencyKeyLength = 255

const (
	orderEventsTopic           = "order.events"
	orderCreatedEventName      = "order.created"
	orderConfirmedEventName    = "order.confirmed"
	orderCancelledEventName    = "order.cancelled"
	orderAggregateType         = "order"
	orderEventSchemaVersion    = "1"
	outboxHeaderIdempotencyKey = "idempotencyKey"
	replayNonTerminalRetries   = 3
	defaultPaymentFailureCode  = "payment_declined"
)

const replayNonTerminalRetryDelay = 10 * time.Millisecond

type TransactionRepos struct {
	Orders    outbound.OrderRepository
	Saga      outbound.OrderSagaStateRepository
	Publisher outbound.EventPublisher
}

type Service struct {
	orders           outbound.OrderRepository
	saga             outbound.OrderSagaStateRepository
	snapshots        outbound.CheckoutSnapshotRepository
	stock            outbound.StockReservationService
	stockRelease     outbound.StockReleaseService
	payment          outbound.CheckoutPaymentService
	idempotencyGuard outbound.CheckoutIdempotencyGuard
	publisher        outbound.EventPublisher
	txProvider       tx.Provider[TransactionRepos]
	producer         string
}

type CheckoutInput struct {
	UserID         uuid.UUID
	IdempotencyKey string
	PaymentMethod  *string
	CorrelationID  string
	CausationID    string
}

type GetOrderInput struct {
	UserID  uuid.UUID
	OrderID uuid.UUID
}

type HandlePaymentSucceededInput struct {
	OrderID uuid.UUID
}

type HandlePaymentFailedInput struct {
	OrderID     uuid.UUID
	FailureCode string
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
		producer:         "order-svc",
	}
}

func (s *Service) WithEventing(
	publisher outbound.EventPublisher,
	txProvider tx.Provider[TransactionRepos],
	producer string,
) *Service {
	if s == nil {
		return nil
	}

	if publisher != nil {
		s.publisher = publisher
	}

	if txProvider != nil {
		s.txProvider = txProvider
	}

	if trimmedProducer := strings.TrimSpace(producer); trimmedProducer != "" {
		s.producer = trimmedProducer
	}

	return s
}

func (s *Service) Checkout(ctx context.Context, input CheckoutInput) (outbound.Order, error) {
	if err := s.validateInput(input); err != nil {
		return outbound.Order{}, err
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)

	ctx = withEventMetadata(ctx, eventMetadata{
		CorrelationID: strings.TrimSpace(input.CorrelationID),
		CausationID:   strings.TrimSpace(input.CausationID),
	})

	normalizedPaymentMethod := normalizePaymentMethod(input.PaymentMethod)

	existing, found, err := s.replayExistingOrderIfPresent(ctx, input.UserID, idempotencyKey, normalizedPaymentMethod)
	if err != nil {
		return outbound.Order{}, err
	}
	if found {
		return existing, nil
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

	createdOrder, err := s.createOrderAndPublishCreatedEvent(
		ctx,
		input.UserID,
		idempotencyKey,
		normalizedPaymentMethod,
		snapshot,
	)
	if err != nil {
		if errors.Is(err, outbound.ErrOrderIdempotencyConflict) {
			replayedOrder, foundReplay, replayErr := s.replayExistingOrderIfPresent(
				ctx,
				input.UserID,
				idempotencyKey,
				normalizedPaymentMethod,
			)
			if replayErr != nil {
				return outbound.Order{}, replayErr
			}
			if foundReplay {
				return replayedOrder, nil
			}

			return outbound.Order{}, wrapCode(
				CheckoutErrorCodeWrongIdempotencyKeyPayload,
				"replay order after idempotency conflict",
				outbound.ErrOrderIdempotencyConflict,
			)
		}

		return outbound.Order{}, err
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
		if compErr := s.compensateStockFailure(ctx, order, code); compErr != nil {
			return outbound.Order{}, compErr
		}

		return outbound.Order{}, wrapCode(code, "reserve stock", err)
	}

	if _, err := s.transitionStockStage(ctx, createdOrder.OrderID, domain.SagaStageSucceeded); err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensatePostStockReservationFailure(ctx, order, reserveStockInput, code); compErr != nil {
			return outbound.Order{}, &CheckoutError{
				Code: code,
				Err:  fmt.Errorf("transition stock stage to succeeded: %w", errors.Join(err, compErr)),
			}
		}

		return outbound.Order{}, wrapCode(code, "transition stock stage to succeeded", err)
	}

	nextOrder, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusAwaitingPayment, nil)
	if err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensatePostStockReservationFailure(ctx, order, reserveStockInput, code); compErr != nil {
			return outbound.Order{}, &CheckoutError{
				Code: code,
				Err:  fmt.Errorf("transition order status to awaiting payment: %w", errors.Join(err, compErr)),
			}
		}

		return outbound.Order{}, wrapCode(code, "transition order status to awaiting payment", err)
	}
	order = nextOrder

	if _, err := s.transitionPaymentStage(ctx, createdOrder.OrderID, domain.SagaStageRequested); err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensatePaymentFailure(ctx, order, reserveStockInput, code); compErr != nil {
			return outbound.Order{}, &CheckoutError{
				Code: code,
				Err:  fmt.Errorf("transition payment stage to requested: %w", errors.Join(err, compErr)),
			}
		}

		return outbound.Order{}, wrapCode(code, "transition payment stage to requested", err)
	}

	if err := s.payment.InitiatePayment(ctx, toInitiatePaymentInput(order)); err != nil {
		code := mapCheckoutCode(err)
		if compErr := s.compensatePaymentFailure(ctx, order, reserveStockInput, code); compErr != nil {
			return outbound.Order{}, &CheckoutError{
				Code: code,
				Err:  fmt.Errorf("initiate payment: %w", errors.Join(err, compErr)),
			}
		}

		return outbound.Order{}, wrapCode(code, "initiate payment", err)
	}

	return order, nil
}

func (s *Service) replayExistingOrderIfPresent(
	ctx context.Context,
	userID uuid.UUID,
	idempotencyKey string,
	paymentMethod string,
) (outbound.Order, bool, error) {
	existing, err := s.orders.GetByUserIDAndIdempotencyKey(ctx, userID, idempotencyKey)
	if err != nil {
		if errors.Is(err, outbound.ErrOrderNotFound) {
			return outbound.Order{}, false, nil
		}

		return outbound.Order{}, false, wrapCode(
			CheckoutErrorCodeInternal,
			"get order by user id and idempotency key",
			err,
		)
	}

	if guardErr := s.idempotencyGuard.ValidateCheckoutIdempotency(
		ctx,
		outbound.ValidateCheckoutIdempotencyInput{
			UserID:         userID,
			IdempotencyKey: idempotencyKey,
			Payload: outbound.CheckoutIdempotencyPayload{
				PaymentMethod: paymentMethod,
			},
		},
	); guardErr != nil {
		return outbound.Order{}, false, wrapCode(mapCheckoutCode(guardErr), "validate checkout idempotency payload", guardErr)
	}

	if !isReplayableIdempotentStatus(existing.Status) {
		for range replayNonTerminalRetries {
			if err := sleepWithContext(ctx, replayNonTerminalRetryDelay); err != nil {
				return outbound.Order{}, false, wrapCode(
					CheckoutErrorCodeInternal,
					"wait before replaying non-terminal order",
					err,
				)
			}

			reloaded, getErr := s.orders.GetByUserIDAndIdempotencyKey(ctx, userID, idempotencyKey)
			if getErr != nil {
				if errors.Is(getErr, outbound.ErrOrderNotFound) {
					return outbound.Order{}, false, nil
				}

				return outbound.Order{}, false, wrapCode(
					CheckoutErrorCodeInternal,
					"reload order by user id and idempotency key",
					getErr,
				)
			}

			if isReplayableIdempotentStatus(reloaded.Status) {
				return reloaded, true, nil
			}
		}

		return outbound.Order{}, false, wrapCode(
			CheckoutErrorCodeConflict,
			"replay non-terminal order is blocked",
			outbound.ErrOrderInvalidStatusTransition,
		)
	}

	return existing, true, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) GetOrder(ctx context.Context, input GetOrderInput) (outbound.Order, error) {
	if input.UserID == uuid.Nil {
		return outbound.Order{}, newCodeError(CheckoutErrorCodeInvalidArgument, "get order input user_id is required")
	}

	if input.OrderID == uuid.Nil {
		return outbound.Order{}, newCodeError(CheckoutErrorCodeInvalidArgument, "get order input order_id is required")
	}

	order, err := s.orders.GetByID(ctx, input.OrderID)
	if err != nil {
		if errors.Is(err, outbound.ErrOrderNotFound) {
			return outbound.Order{}, wrapCode(CheckoutErrorCodeCartNotFound, "get order by id", err)
		}

		return outbound.Order{}, wrapCode(CheckoutErrorCodeInternal, "get order by id", err)
	}

	if order.UserID != input.UserID {
		return outbound.Order{}, wrapCode(CheckoutErrorCodeCartNotFound, "get order by id", outbound.ErrOrderNotFound)
	}

	return order, nil
}

func (s *Service) HandlePaymentSucceeded(ctx context.Context, input HandlePaymentSucceededInput) error {
	if input.OrderID == uuid.Nil {
		return newCodeError(CheckoutErrorCodeInvalidArgument, "payment succeeded input order_id is required")
	}

	order, err := s.orders.GetByID(ctx, input.OrderID)
	if err != nil {
		if errors.Is(err, outbound.ErrOrderNotFound) {
			return nil
		}

		return wrapCode(CheckoutErrorCodeInternal, "get order by id", err)
	}

	if _, err := s.saga.TransitionPaymentStageToSucceeded(ctx, input.OrderID); err != nil {
		if !isReplaySafeTransitionError(err) {
			return wrapCode(mapCheckoutCode(err), "transition payment stage to succeeded", err)
		}
	}

	if _, err := s.saga.ClearLastErrorCode(ctx, input.OrderID); err != nil {
		return wrapCode(mapCheckoutCode(err), "clear order saga last error code", err)
	}

	if order.Status == outbound.OrderStatusConfirmed || order.Status == outbound.OrderStatusCancelled {
		return nil
	}

	if _, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusConfirmed, nil); err != nil {
		if isReplaySafeTransitionError(err) {
			return nil
		}

		return err
	}

	return nil
}

func (s *Service) HandlePaymentFailed(ctx context.Context, input HandlePaymentFailedInput) error {
	if input.OrderID == uuid.Nil {
		return newCodeError(CheckoutErrorCodeInvalidArgument, "payment failed input order_id is required")
	}

	failureCode := strings.TrimSpace(input.FailureCode)
	if failureCode == "" {
		failureCode = defaultPaymentFailureCode
	}

	order, err := s.orders.GetByID(ctx, input.OrderID)
	if err != nil {
		if errors.Is(err, outbound.ErrOrderNotFound) {
			return nil
		}

		return wrapCode(CheckoutErrorCodeInternal, "get order by id", err)
	}

	if order.Status == outbound.OrderStatusConfirmed {
		return nil
	}

	if _, err := s.saga.TransitionPaymentStageToFailed(ctx, input.OrderID); err != nil {
		if !isReplaySafeTransitionError(err) {
			return wrapCode(mapCheckoutCode(err), "transition payment stage to failed", err)
		}
	}

	if _, err := s.saga.SetLastErrorCode(ctx, input.OrderID, failureCode); err != nil {
		return wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
	}

	if order.Status != outbound.OrderStatusCancelled {
		reasonCode := failureCode
		if _, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusCancelled, &reasonCode); err != nil {
			if !isReplaySafeTransitionError(err) {
				return err
			}

			reloadedOrder, getErr := s.orders.GetByID(ctx, input.OrderID)
			if getErr != nil {
				if errors.Is(getErr, outbound.ErrOrderNotFound) {
					return nil
				}

				return wrapCode(CheckoutErrorCodeInternal, "reload order by id after cancel conflict", getErr)
			}

			switch reloadedOrder.Status {
			case outbound.OrderStatusConfirmed:
				return nil
			case outbound.OrderStatusCancelled:
				order = reloadedOrder
			}
		}
	}

	if order.Status == outbound.OrderStatusConfirmed {
		return nil
	}

	if err := s.stockRelease.ReleaseStock(ctx, toReleaseStockInputFromOrder(order)); err != nil {
		return fmt.Errorf("release stock after payment failure: %w", err)
	}

	return nil
}

func (s *Service) compensateStockFailure(
	ctx context.Context,
	order outbound.Order,
	code CheckoutErrorCode,
) error {
	if _, err := s.transitionStockStage(ctx, order.OrderID, domain.SagaStageFailed); err != nil {
		return err
	}

	if _, err := s.saga.SetLastErrorCode(ctx, order.OrderID, string(code)); err != nil {
		return wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
	}

	reason := string(code)
	_, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusCancelled, &reason)
	if err != nil {
		return err
	}

	return nil
}

func (s *Service) compensatePaymentFailure(
	ctx context.Context,
	order outbound.Order,
	releaseStockInput outbound.ReserveStockInput,
	code CheckoutErrorCode,
) error {
	var compensationErr error

	if _, err := s.transitionPaymentStage(ctx, order.OrderID, domain.SagaStageFailed); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("transition payment stage to failed during compensation: %w", err))
	}

	if _, err := s.saga.SetLastErrorCode(ctx, order.OrderID, string(code)); err != nil {
		wrapped := wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
		compensationErr = errors.Join(compensationErr, fmt.Errorf("set order saga last error code during payment compensation: %w", wrapped))
	}

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

func (s *Service) compensatePostStockReservationFailure(
	ctx context.Context,
	order outbound.Order,
	releaseStockInput outbound.ReserveStockInput,
	code CheckoutErrorCode,
) error {
	var compensationErr error

	if _, err := s.transitionStockStage(ctx, order.OrderID, domain.SagaStageFailed); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("transition stock stage to failed during compensation: %w", err))
	}

	if _, err := s.saga.SetLastErrorCode(ctx, order.OrderID, string(code)); err != nil {
		wrapped := wrapCode(mapCheckoutCode(err), "set order saga last error code", err)
		compensationErr = errors.Join(compensationErr, fmt.Errorf("set order saga last error code during stock compensation: %w", wrapped))
	}

	reason := string(code)
	if _, err := s.transitionOrderStatus(ctx, order, domain.OrderStatusCancelled, &reason); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("cancel order after stock reservation failure: %w", err))
	}

	if err := s.stockRelease.ReleaseStock(ctx, toReleaseStockInput(releaseStockInput)); err != nil {
		compensationErr = errors.Join(compensationErr, fmt.Errorf("release stock after stock reservation failure: %w", err))
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

	updated := outbound.Order{}
	err = s.withTransactionRepos(ctx, func(repos TransactionRepos) error {
		nextOrder, transitionErr := repos.Orders.TransitionStatus(
			ctx,
			order.OrderID,
			toOutboundOrderStatus(fromStatus),
			toOutboundOrderStatus(next),
		)
		if transitionErr != nil {
			return wrapCode(mapCheckoutCode(transitionErr), "transition order status", transitionErr)
		}

		nextOrder.Items = order.Items
		if nextOrder.UserID == uuid.Nil {
			nextOrder.UserID = order.UserID
		}
		if nextOrder.Currency == "" {
			nextOrder.Currency = order.Currency
		}
		if nextOrder.TotalAmount == 0 {
			nextOrder.TotalAmount = order.TotalAmount
		}

		fromStatusOutbound := toOutboundOrderStatus(fromStatus)
		if _, historyErr := repos.Orders.AppendStatusHistory(ctx, order.OrderID, &fromStatusOutbound, nextOrder.Status, reasonCode); historyErr != nil {
			return wrapCode(mapCheckoutCode(historyErr), "append order status history", historyErr)
		}

		if next == domain.OrderStatusCancelled {
			cancelledEvent, eventErr := s.newOrderCancelledEvent(ctx, nextOrder, reasonCode)
			if eventErr != nil {
				return wrapCode(CheckoutErrorCodeInternal, "map order cancelled event", eventErr)
			}

			if publishErr := repos.Publisher.Publish(ctx, cancelledEvent); publishErr != nil {
				return wrapCode(mapCheckoutCode(publishErr), "publish order cancelled event", publishErr)
			}
		}

		if next == domain.OrderStatusConfirmed {
			confirmedEvent, eventErr := s.newOrderConfirmedEvent(ctx, nextOrder)
			if eventErr != nil {
				return wrapCode(CheckoutErrorCodeInternal, "map order confirmed event", eventErr)
			}

			if publishErr := repos.Publisher.Publish(ctx, confirmedEvent); publishErr != nil {
				return wrapCode(mapCheckoutCode(publishErr), "publish order confirmed event", publishErr)
			}
		}

		updated = nextOrder
		return nil
	})
	if err != nil {
		return outbound.Order{}, err
	}

	return updated, nil
}

func (s *Service) createOrderAndPublishCreatedEvent(
	ctx context.Context,
	userID uuid.UUID,
	idempotencyKey string,
	paymentMethod string,
	snapshot outbound.CheckoutSnapshot,
) (outbound.Order, error) {
	createdOrder := outbound.Order{}
	input := toCreateOrderInput(userID, idempotencyKey, paymentMethod, snapshot)

	err := s.withTransactionRepos(ctx, func(repos TransactionRepos) error {
		order, createErr := repos.Orders.CreateWithItems(ctx, input)
		if createErr != nil {
			return wrapCode(mapCheckoutCode(createErr), "create order with items", createErr)
		}

		createdEvent, eventErr := s.newOrderCreatedEvent(ctx, order, idempotencyKey)
		if eventErr != nil {
			return wrapCode(CheckoutErrorCodeInternal, "map order created event", eventErr)
		}

		if publishErr := repos.Publisher.Publish(ctx, createdEvent); publishErr != nil {
			return wrapCode(mapCheckoutCode(publishErr), "publish order created event", publishErr)
		}

		createdOrder = order
		return nil
	})
	if err != nil {
		return outbound.Order{}, err
	}

	return createdOrder, nil
}

func (s *Service) withTransactionRepos(ctx context.Context, fn func(TransactionRepos) error) error {
	repos := TransactionRepos{
		Orders:    s.orders,
		Saga:      s.saga,
		Publisher: s.resolvePublisher(),
	}

	if s.txProvider == nil {
		return fn(repos)
	}

	return s.txProvider.WithTransaction(ctx, nil, func(uow tx.UnitOfWork[TransactionRepos]) error {
		return fn(uow.Repos())
	})
}

func (s *Service) resolvePublisher() outbound.EventPublisher {
	if s.publisher != nil {
		return s.publisher
	}

	return noopEventPublisher{}
}

func (s *Service) newOrderCreatedEvent(
	ctx context.Context,
	order outbound.Order,
	idempotencyKey string,
) (domain.DomainEvent, error) {
	if order.OrderID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("order_id is required")
	}

	if order.UserID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("user_id is required")
	}

	correlationID := correlationIDFromContext(ctx, order.OrderID.String())
	causationID := causationIDFromContext(ctx)
	if causationID == "" {
		causationID = strings.TrimSpace(idempotencyKey)
	}
	if causationID == "" {
		causationID = correlationID
	}

	items := make([]domain.OrderItemSnapshot, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, domain.OrderItemSnapshot{
			ProductID: item.ProductID.String(),
			SKU:       item.SKU,
			Name:      item.Name,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice,
			LineTotal: item.LineTotal,
			Currency:  item.Currency,
		})
	}

	headers := map[string]string{}
	if idempotency := strings.TrimSpace(idempotencyKey); idempotency != "" {
		headers[outboxHeaderIdempotencyKey] = idempotency
	}

	return domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     orderCreatedEventName,
		Producer:      s.producer,
		OccurredAt:    timeNowUTC(),
		CorrelationID: correlationID,
		CausationID:   causationID,
		SchemaVersion: orderEventSchemaVersion,
		AggregateType: orderAggregateType,
		AggregateID:   order.OrderID.String(),
		Topic:         orderEventsTopic,
		Key:           order.OrderID.String(),
		Payload: domain.OrderCreatedPayload{
			OrderID:     order.OrderID.String(),
			UserID:      order.UserID.String(),
			Status:      domain.OrderStatus(order.Status),
			Currency:    order.Currency,
			TotalAmount: order.TotalAmount,
			Items:       items,
		},
		Headers: headers,
	}, nil
}

func (s *Service) newOrderCancelledEvent(
	ctx context.Context,
	order outbound.Order,
	reasonCode *string,
) (domain.DomainEvent, error) {
	if order.OrderID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("order_id is required")
	}

	if order.UserID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("user_id is required")
	}

	cancelReasonCode := ""
	if reasonCode != nil {
		cancelReasonCode = strings.TrimSpace(*reasonCode)
	}

	correlationID := correlationIDFromContext(ctx, order.OrderID.String())
	causationID := causationIDFromContext(ctx)
	if causationID == "" {
		causationID = correlationID
	}

	now := timeNowUTC()

	return domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     orderCancelledEventName,
		Producer:      s.producer,
		OccurredAt:    now,
		CorrelationID: correlationID,
		CausationID:   causationID,
		SchemaVersion: orderEventSchemaVersion,
		AggregateType: orderAggregateType,
		AggregateID:   order.OrderID.String(),
		Topic:         orderEventsTopic,
		Key:           order.OrderID.String(),
		Payload: domain.OrderCancelledPayload{
			OrderID:             order.OrderID.String(),
			UserID:              order.UserID.String(),
			Status:              domain.OrderStatus(order.Status),
			CancelReasonCode:    cancelReasonCode,
			CancelReasonMessage: cancelReasonCode,
			CancelledAt:         now,
		},
		Headers: map[string]string{},
	}, nil
}

func (s *Service) newOrderConfirmedEvent(
	ctx context.Context,
	order outbound.Order,
) (domain.DomainEvent, error) {
	if order.OrderID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("order_id is required")
	}

	if order.UserID == uuid.Nil {
		return domain.DomainEvent{}, fmt.Errorf("user_id is required")
	}

	correlationID := correlationIDFromContext(ctx, order.OrderID.String())
	causationID := causationIDFromContext(ctx)
	if causationID == "" {
		causationID = correlationID
	}

	now := timeNowUTC()

	return domain.DomainEvent{
		EventID:       uuid.NewString(),
		EventName:     orderConfirmedEventName,
		Producer:      s.producer,
		OccurredAt:    now,
		CorrelationID: correlationID,
		CausationID:   causationID,
		SchemaVersion: orderEventSchemaVersion,
		AggregateType: orderAggregateType,
		AggregateID:   order.OrderID.String(),
		Topic:         orderEventsTopic,
		Key:           order.OrderID.String(),
		Payload: domain.OrderConfirmedPayload{
			OrderID:     order.OrderID.String(),
			UserID:      order.UserID.String(),
			Status:      domain.OrderStatus(order.Status),
			Currency:    order.Currency,
			TotalAmount: order.TotalAmount,
			ConfirmedAt: now,
		},
		Headers: map[string]string{},
	}, nil
}

func correlationIDFromContext(ctx context.Context, fallback string) string {
	correlationID := strings.TrimSpace(eventMetadataFromContext(ctx).CorrelationID)
	if correlationID != "" {
		return correlationID
	}

	if strings.TrimSpace(fallback) != "" {
		return fallback
	}

	return uuid.NewString()
}

func causationIDFromContext(ctx context.Context) string {
	return strings.TrimSpace(eventMetadataFromContext(ctx).CausationID)
}

type eventMetadata struct {
	CorrelationID string
	CausationID   string
}

type eventMetadataKey struct{}

func withEventMetadata(ctx context.Context, metadata eventMetadata) context.Context {
	return context.WithValue(ctx, eventMetadataKey{}, metadata)
}

func eventMetadataFromContext(ctx context.Context) eventMetadata {
	metadata, _ := ctx.Value(eventMetadataKey{}).(eventMetadata)
	return metadata
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
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

func toCreateOrderInput(userID uuid.UUID, idempotencyKey string, paymentMethod string, snapshot outbound.CheckoutSnapshot) outbound.CreateOrderInput {
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
		OrderID:            uuid.New(),
		UserID:             userID,
		Status:             outbound.OrderStatusPending,
		Currency:           snapshot.Currency,
		TotalAmount:        snapshot.TotalAmount,
		IdempotencyKey:     idempotencyKey,
		PayloadFingerprint: checkoutPayloadFingerprint(outbound.CheckoutIdempotencyPayload{PaymentMethod: paymentMethod}),
		Items:              items,
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

func isReplayableIdempotentStatus(status outbound.OrderStatus) bool {
	switch status {
	case outbound.OrderStatusAwaitingPayment, outbound.OrderStatusConfirmed, outbound.OrderStatusCancelled:
		return true
	default:
		return false
	}
}

func isReplaySafeTransitionError(err error) bool {
	return errors.Is(err, outbound.ErrOrderInvalidStatusTransition) ||
		errors.Is(err, outbound.ErrOrderSagaStateInvalidTransition) ||
		errors.Is(err, domain.ErrInvalidOrderStatusTransition) ||
		errors.Is(err, domain.ErrCancelledOrderTerminal) ||
		errors.Is(err, domain.ErrConfirmedOrderImmutable) ||
		errors.Is(err, domain.ErrInvalidSagaStageTransition) ||
		errors.Is(err, domain.ErrSagaTerminalStateConflict)
}

func toReleaseStockInputFromOrder(order outbound.Order) outbound.ReleaseStockInput {
	items := make([]outbound.ReleaseStockItem, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, outbound.ReleaseStockItem{
			ProductID: item.ProductID,
			SKU:       item.SKU,
			Quantity:  item.Quantity,
		})
	}

	return outbound.ReleaseStockInput{
		OrderID: order.OrderID,
		UserID:  order.UserID,
		Items:   items,
	}
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

type noopEventPublisher struct{}

func (noopEventPublisher) Publish(context.Context, domain.DomainEvent) error {
	return nil
}
