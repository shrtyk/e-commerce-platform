package domain

import (
	"errors"
	"fmt"
)

type OrderStatus string

const (
	OrderStatusPending         OrderStatus = "pending"
	OrderStatusAwaitingStock   OrderStatus = "awaiting_stock"
	OrderStatusAwaitingPayment OrderStatus = "awaiting_payment"
	OrderStatusConfirmed       OrderStatus = "confirmed"
	OrderStatusCancelled       OrderStatus = "cancelled"
)

type SagaStage string

const (
	SagaStageNotStarted SagaStage = "not_started"
	SagaStageRequested  SagaStage = "requested"
	SagaStageSucceeded  SagaStage = "succeeded"
	SagaStageFailed     SagaStage = "failed"
)

var (
	ErrUnknownOrderStatus              = errors.New("unknown order status")
	ErrInvalidOrderStatusTransition    = errors.New("invalid order status transition")
	ErrConfirmedOrderImmutable         = errors.New("confirmed order is immutable")
	ErrCancelledOrderTerminal          = errors.New("cancelled order is terminal")
	ErrUnknownSagaStage                = errors.New("unknown saga stage")
	ErrInvalidSagaStageTransition      = errors.New("invalid saga stage transition")
	ErrSagaTerminalStateConflict       = errors.New("saga terminal state conflict")
	ErrSagaPaymentRequiresStockSuccess = errors.New("payment transition requires successful stock stage")
)

func TransitionOrderStatus(fromStatus OrderStatus, toStatus OrderStatus) (OrderStatus, error) {
	if !isKnownOrderStatus(fromStatus) {
		return "", fmt.Errorf("validate from order status %q: %w", fromStatus, ErrUnknownOrderStatus)
	}

	if !isKnownOrderStatus(toStatus) {
		return "", fmt.Errorf("validate to order status %q: %w", toStatus, ErrUnknownOrderStatus)
	}

	if fromStatus == toStatus {
		return toStatus, nil
	}

	if fromStatus == OrderStatusConfirmed && toStatus != OrderStatusConfirmed {
		return "", fmt.Errorf(
			"transition order status from %q to %q: %w",
			fromStatus,
			toStatus,
			ErrConfirmedOrderImmutable,
		)
	}

	if fromStatus == OrderStatusCancelled && toStatus != OrderStatusCancelled {
		return "", fmt.Errorf(
			"transition order status from %q to %q: %w",
			fromStatus,
			toStatus,
			ErrCancelledOrderTerminal,
		)
	}

	if !isAllowedOrderStatusTransition(fromStatus, toStatus) {
		return "", fmt.Errorf(
			"transition order status from %q to %q: %w",
			fromStatus,
			toStatus,
			ErrInvalidOrderStatusTransition,
		)
	}

	return toStatus, nil
}

func TransitionStockStage(fromStage SagaStage, toStage SagaStage) (SagaStage, error) {
	return transitionSagaStage(fromStage, toStage, "stock")
}

func TransitionPaymentStage(stockStage SagaStage, fromPaymentStage SagaStage, toPaymentStage SagaStage) (SagaStage, error) {
	if !isKnownSagaStage(stockStage) {
		return "", fmt.Errorf("validate stock stage %q: %w", stockStage, ErrUnknownSagaStage)
	}

	if !isKnownSagaStage(fromPaymentStage) {
		return "", fmt.Errorf("validate from payment stage %q: %w", fromPaymentStage, ErrUnknownSagaStage)
	}

	if !isKnownSagaStage(toPaymentStage) {
		return "", fmt.Errorf("validate to payment stage %q: %w", toPaymentStage, ErrUnknownSagaStage)
	}

	if toPaymentStage != SagaStageNotStarted && stockStage != SagaStageSucceeded {
		return "", fmt.Errorf(
			"transition payment stage from %q to %q with stock stage %q: %w",
			fromPaymentStage,
			toPaymentStage,
			stockStage,
			ErrSagaPaymentRequiresStockSuccess,
		)
	}

	return transitionSagaStage(fromPaymentStage, toPaymentStage, "payment")
}

func transitionSagaStage(fromStage SagaStage, toStage SagaStage, stageName string) (SagaStage, error) {
	if !isKnownSagaStage(fromStage) {
		return "", fmt.Errorf("validate from %s stage %q: %w", stageName, fromStage, ErrUnknownSagaStage)
	}

	if !isKnownSagaStage(toStage) {
		return "", fmt.Errorf("validate to %s stage %q: %w", stageName, toStage, ErrUnknownSagaStage)
	}

	if fromStage == toStage {
		return toStage, nil
	}

	if isTerminalSagaStage(fromStage) && isTerminalSagaStage(toStage) {
		return "", fmt.Errorf(
			"transition %s stage from %q to %q: %w",
			stageName,
			fromStage,
			toStage,
			ErrSagaTerminalStateConflict,
		)
	}

	if !isAllowedSagaStageTransition(fromStage, toStage) {
		return "", fmt.Errorf(
			"transition %s stage from %q to %q: %w",
			stageName,
			fromStage,
			toStage,
			ErrInvalidSagaStageTransition,
		)
	}

	return toStage, nil
}

func isKnownOrderStatus(status OrderStatus) bool {
	switch status {
	case OrderStatusPending,
		OrderStatusAwaitingStock,
		OrderStatusAwaitingPayment,
		OrderStatusConfirmed,
		OrderStatusCancelled:
		return true
	default:
		return false
	}
}

func isAllowedOrderStatusTransition(fromStatus OrderStatus, toStatus OrderStatus) bool {
	switch fromStatus {
	case OrderStatusPending:
		return toStatus == OrderStatusAwaitingStock
	case OrderStatusAwaitingStock:
		return toStatus == OrderStatusAwaitingPayment || toStatus == OrderStatusCancelled
	case OrderStatusAwaitingPayment:
		return toStatus == OrderStatusConfirmed || toStatus == OrderStatusCancelled
	default:
		return false
	}
}

func isKnownSagaStage(stage SagaStage) bool {
	switch stage {
	case SagaStageNotStarted, SagaStageRequested, SagaStageSucceeded, SagaStageFailed:
		return true
	default:
		return false
	}
}

func isTerminalSagaStage(stage SagaStage) bool {
	return stage == SagaStageSucceeded || stage == SagaStageFailed
}

func isAllowedSagaStageTransition(fromStage SagaStage, toStage SagaStage) bool {
	switch fromStage {
	case SagaStageNotStarted:
		return toStage == SagaStageRequested
	case SagaStageRequested:
		return toStage == SagaStageSucceeded || toStage == SagaStageFailed
	default:
		return false
	}
}
