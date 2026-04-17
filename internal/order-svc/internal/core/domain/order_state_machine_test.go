package domain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransitionOrderStatus(t *testing.T) {
	tests := []struct {
		name       string
		fromStatus OrderStatus
		toStatus   OrderStatus
		want       OrderStatus
		errIs      error
	}{
		{
			name:       "pending to awaiting_stock",
			fromStatus: OrderStatusPending,
			toStatus:   OrderStatusAwaitingStock,
			want:       OrderStatusAwaitingStock,
		},
		{
			name:       "awaiting_stock to awaiting_payment",
			fromStatus: OrderStatusAwaitingStock,
			toStatus:   OrderStatusAwaitingPayment,
			want:       OrderStatusAwaitingPayment,
		},
		{
			name:       "awaiting_payment to confirmed",
			fromStatus: OrderStatusAwaitingPayment,
			toStatus:   OrderStatusConfirmed,
			want:       OrderStatusConfirmed,
		},
		{
			name:       "awaiting_stock to cancelled",
			fromStatus: OrderStatusAwaitingStock,
			toStatus:   OrderStatusCancelled,
			want:       OrderStatusCancelled,
		},
		{
			name:       "awaiting_payment to cancelled",
			fromStatus: OrderStatusAwaitingPayment,
			toStatus:   OrderStatusCancelled,
			want:       OrderStatusCancelled,
		},
		{
			name:       "confirmed immutable",
			fromStatus: OrderStatusConfirmed,
			toStatus:   OrderStatusCancelled,
			errIs:      ErrConfirmedOrderImmutable,
		},
		{
			name:       "cancelled terminal cannot confirm",
			fromStatus: OrderStatusCancelled,
			toStatus:   OrderStatusConfirmed,
			errIs:      ErrCancelledOrderTerminal,
		},
		{
			name:       "cancelled terminal cannot progress",
			fromStatus: OrderStatusCancelled,
			toStatus:   OrderStatusAwaitingPayment,
			errIs:      ErrCancelledOrderTerminal,
		},
		{
			name:       "confirmed idempotent repeat accepted",
			fromStatus: OrderStatusConfirmed,
			toStatus:   OrderStatusConfirmed,
			want:       OrderStatusConfirmed,
		},
		{
			name:       "cancelled idempotent repeat accepted",
			fromStatus: OrderStatusCancelled,
			toStatus:   OrderStatusCancelled,
			want:       OrderStatusCancelled,
		},
		{
			name:       "invalid skip pending to awaiting_payment",
			fromStatus: OrderStatusPending,
			toStatus:   OrderStatusAwaitingPayment,
			errIs:      ErrInvalidOrderStatusTransition,
		},
		{
			name:       "pending idempotent repeat accepted",
			fromStatus: OrderStatusPending,
			toStatus:   OrderStatusPending,
			want:       OrderStatusPending,
		},
		{
			name:       "unknown from status",
			fromStatus: OrderStatus("unknown"),
			toStatus:   OrderStatusPending,
			errIs:      ErrUnknownOrderStatus,
		},
		{
			name:       "unknown to status",
			fromStatus: OrderStatusPending,
			toStatus:   OrderStatus("unknown"),
			errIs:      ErrUnknownOrderStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TransitionOrderStatus(tt.fromStatus, tt.toStatus)

			if tt.errIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errIs)
				require.Zero(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTransitionStockStage(t *testing.T) {
	tests := []struct {
		name      string
		fromStage SagaStage
		toStage   SagaStage
		want      SagaStage
		errIs     error
	}{
		{
			name:      "not_started to requested",
			fromStage: SagaStageNotStarted,
			toStage:   SagaStageRequested,
			want:      SagaStageRequested,
		},
		{
			name:      "requested to succeeded",
			fromStage: SagaStageRequested,
			toStage:   SagaStageSucceeded,
			want:      SagaStageSucceeded,
		},
		{
			name:      "requested to failed",
			fromStage: SagaStageRequested,
			toStage:   SagaStageFailed,
			want:      SagaStageFailed,
		},
		{
			name:      "idempotent repeat accepted",
			fromStage: SagaStageSucceeded,
			toStage:   SagaStageSucceeded,
			want:      SagaStageSucceeded,
		},
		{
			name:      "cannot flip terminal succeeded to failed",
			fromStage: SagaStageSucceeded,
			toStage:   SagaStageFailed,
			errIs:     ErrSagaTerminalStateConflict,
		},
		{
			name:      "cannot flip terminal failed to succeeded",
			fromStage: SagaStageFailed,
			toStage:   SagaStageSucceeded,
			errIs:     ErrSagaTerminalStateConflict,
		},
		{
			name:      "cannot skip to terminal from not_started",
			fromStage: SagaStageNotStarted,
			toStage:   SagaStageSucceeded,
			errIs:     ErrInvalidSagaStageTransition,
		},
		{
			name:      "unknown from stage",
			fromStage: SagaStage("unknown"),
			toStage:   SagaStageRequested,
			errIs:     ErrUnknownSagaStage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TransitionStockStage(tt.fromStage, tt.toStage)

			if tt.errIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errIs)
				require.Zero(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTransitionPaymentStage(t *testing.T) {
	tests := []struct {
		name             string
		stockStage       SagaStage
		fromPaymentStage SagaStage
		toPaymentStage   SagaStage
		want             SagaStage
		errIs            error
	}{
		{
			name:             "requested allowed when stock succeeded",
			stockStage:       SagaStageSucceeded,
			fromPaymentStage: SagaStageNotStarted,
			toPaymentStage:   SagaStageRequested,
			want:             SagaStageRequested,
		},
		{
			name:             "succeeded allowed after requested",
			stockStage:       SagaStageSucceeded,
			fromPaymentStage: SagaStageRequested,
			toPaymentStage:   SagaStageSucceeded,
			want:             SagaStageSucceeded,
		},
		{
			name:             "failed allowed after requested",
			stockStage:       SagaStageSucceeded,
			fromPaymentStage: SagaStageRequested,
			toPaymentStage:   SagaStageFailed,
			want:             SagaStageFailed,
		},
		{
			name:             "idempotent repeat accepted",
			stockStage:       SagaStageSucceeded,
			fromPaymentStage: SagaStageFailed,
			toPaymentStage:   SagaStageFailed,
			want:             SagaStageFailed,
		},
		{
			name:             "stock guard blocks payment requested",
			stockStage:       SagaStageRequested,
			fromPaymentStage: SagaStageNotStarted,
			toPaymentStage:   SagaStageRequested,
			errIs:            ErrSagaPaymentRequiresStockSuccess,
		},
		{
			name:             "stock guard blocks payment terminal",
			stockStage:       SagaStageNotStarted,
			fromPaymentStage: SagaStageRequested,
			toPaymentStage:   SagaStageSucceeded,
			errIs:            ErrSagaPaymentRequiresStockSuccess,
		},
		{
			name:             "cannot flip payment terminal",
			stockStage:       SagaStageSucceeded,
			fromPaymentStage: SagaStageSucceeded,
			toPaymentStage:   SagaStageFailed,
			errIs:            ErrSagaTerminalStateConflict,
		},
		{
			name:             "unknown stock stage",
			stockStage:       SagaStage("unknown"),
			fromPaymentStage: SagaStageNotStarted,
			toPaymentStage:   SagaStageNotStarted,
			errIs:            ErrUnknownSagaStage,
		},
		{
			name:             "unknown payment stage validated before stock guard",
			stockStage:       SagaStageRequested,
			fromPaymentStage: SagaStageNotStarted,
			toPaymentStage:   SagaStage("unknown"),
			errIs:            ErrUnknownSagaStage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TransitionPaymentStage(tt.stockStage, tt.fromPaymentStage, tt.toPaymentStage)

			if tt.errIs != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.errIs)
				require.Zero(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
