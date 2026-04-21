package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
	outboundmocks "github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound/mocks"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type deliveryProviderStub struct {
	sendFunc func(ctx context.Context, input outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error)
}

func (s deliveryProviderStub) Send(ctx context.Context, input outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
	if s.sendFunc == nil {
		return outbound.SendDeliveryResult{}, nil
	}

	return s.sendFunc(ctx, input)
}

func TestNewNotificationService(t *testing.T) {
	t.Run("panics when delivery request repository nil", func(t *testing.T) {
		require.PanicsWithValue(t, errNilDeliveryRequestsRepository, func() {
			_ = NewNotificationService(nil, outboundmocks.NewMockDeliveryAttemptRepository(t), outboundmocks.NewMockConsumerIdempotencyRepository(t))
		})
	})

	t.Run("panics when delivery attempt repository nil", func(t *testing.T) {
		require.PanicsWithValue(t, errNilDeliveryAttemptsRepository, func() {
			_ = NewNotificationService(outboundmocks.NewMockDeliveryRequestRepository(t), nil, outboundmocks.NewMockConsumerIdempotencyRepository(t))
		})
	})

	t.Run("panics when consumer idempotency repository nil", func(t *testing.T) {
		require.PanicsWithValue(t, errNilConsumerIdempotenciesRepository, func() {
			_ = NewNotificationService(outboundmocks.NewMockDeliveryRequestRepository(t), outboundmocks.NewMockDeliveryAttemptRepository(t), nil)
		})
	})
}

func TestRequestDelivery(t *testing.T) {
	type testCase struct {
		name           string
		input          RequestDeliveryInput
		setupMocks     func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository)
		expectedErr    error
		expectedErrMsg string
		expectedReplay bool
	}

	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	baseInput := RequestDeliveryInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.created",
		SourceEventID:     eventID,
		SourceEventName:   "order.created",
		Channel:           "email",
		Recipient:         "user@example.com",
		TemplateKey:       "order-created",
		IdempotencyKey:    "req-order-1",
	}

	tests := []testCase{
		{
			name:  "creates delivery request",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, nil)
				dr.EXPECT().CreateRequested(testifymock.Anything, outbound.CreateDeliveryRequestInput{
					SourceEventID:   eventID,
					SourceEventName: "order.created",
					Channel:         "email",
					Recipient:       "user@example.com",
					TemplateKey:     "order-created",
					IdempotencyKey:  "req-order-1",
				}).Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					SourceEventID:     eventID,
					SourceEventName:   "order.created",
					Channel:           "email",
					Recipient:         "user@example.com",
					TemplateKey:       "order-created",
					Status:            domain.DeliveryStatusRequested,
					IdempotencyKey:    "req-order-1",
					CreatedAt:         now,
					UpdatedAt:         now,
				}, nil)
				cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
					EventID:           eventID,
					ConsumerGroupName: "notification.order.created",
					DeliveryRequestID: deliveryRequestID,
				}).Return(nil)
			},
		},
		{
			name:           "rejects invalid input",
			input:          RequestDeliveryInput{},
			expectedErr:    ErrInvalidRequestDeliveryInput,
			expectedReplay: false,
		},
		{
			name:  "returns error when idempotency exists check fails",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, errors.New("exists boom"))
			},
			expectedErrMsg: "exists boom",
		},
		{
			name:  "returns existing for idempotent replay",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(true, nil)
				dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, "req-order-1").Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					Status:            domain.DeliveryStatusRequested,
				}, nil)
			},
			expectedReplay: true,
		},
		{
			name:  "maps create invalid arg",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, nil)
				dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg)
			},
			expectedErr: ErrInvalidRequestDeliveryInput,
		},
		{
			name:  "create duplicate reads existing request and creates idempotency marker",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, nil)
				dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{}, outbound.ErrDeliveryRequestDuplicate)
				dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, "req-order-1").Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					Status:            domain.DeliveryStatusRequested,
				}, nil)
				cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
					EventID:           eventID,
					ConsumerGroupName: "notification.order.created",
					DeliveryRequestID: deliveryRequestID,
				}).Return(nil)
			},
			expectedReplay: true,
		},
		{
			name:  "create duplicate treats idempotency duplicate as replay",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, nil)
				dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{}, outbound.ErrDeliveryRequestDuplicate)
				dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, "req-order-1").Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					Status:            domain.DeliveryStatusRequested,
				}, nil)
				cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
					EventID:           eventID,
					ConsumerGroupName: "notification.order.created",
					DeliveryRequestID: deliveryRequestID,
				}).Return(outbound.ErrConsumerIdempotencyDuplicate)
			},
			expectedReplay: true,
		},
		{
			name:  "create duplicate returns idempotency create error",
			input: baseInput,
			setupMocks: func(dr *outboundmocks.MockDeliveryRequestRepository, cid *outboundmocks.MockConsumerIdempotencyRepository) {
				cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.created").Return(false, nil)
				dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{}, outbound.ErrDeliveryRequestDuplicate)
				dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, "req-order-1").Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					Status:            domain.DeliveryStatusRequested,
				}, nil)
				cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
					EventID:           eventID,
					ConsumerGroupName: "notification.order.created",
					DeliveryRequestID: deliveryRequestID,
				}).Return(errors.New("idempotency create boom"))
			},
			expectedErrMsg: "idempotency create boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dr := outboundmocks.NewMockDeliveryRequestRepository(t)
			da := outboundmocks.NewMockDeliveryAttemptRepository(t)
			cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
			svc := NewNotificationService(dr, da, cid)

			if tt.setupMocks != nil {
				tt.setupMocks(dr, cid)
			}

			result, err := svc.RequestDelivery(ctx, tt.input)

			if tt.expectedErr != nil || tt.expectedErrMsg != "" {
				if tt.expectedErr != nil {
					require.ErrorIs(t, err, tt.expectedErr)
				}
				if tt.expectedErrMsg != "" {
					require.ErrorContains(t, err, tt.expectedErrMsg)
				}
				require.Equal(t, RequestDeliveryResult{}, result)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedReplay, result.IdempotentReplay)
		})
	}
}

func TestMarkSent(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	input := MarkSentInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.delivery.results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		AttemptedAt:       now,
	}

	t.Run("marks sent from requested", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		da.EXPECT().Create(testifymock.Anything, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "provider",
			ProviderMessageID: "msg-1",
			AttemptedAt:       now,
		}).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
		dr.EXPECT().MarkSent(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusSent,
		}, nil)
		result, err := svc.MarkSent(ctx, input)

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryStatusSent, result.DeliveryRequest.Status)
		require.False(t, result.IdempotentReplay)
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		result, err := svc.MarkSent(ctx, MarkSentInput{})

		require.ErrorIs(t, err, ErrInvalidMarkSentInput)
		require.Equal(t, MarkSentResult{}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		cid.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	})

	t.Run("rejects invalid transition", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusFailed,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)

		result, err := svc.MarkSent(ctx, input)

		require.ErrorIs(t, err, ErrInvalidDeliveryTransition)
		require.Equal(t, MarkSentResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
	})

	t.Run("idempotent replay on duplicate idempotency marker skips side effects", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(outbound.ErrConsumerIdempotencyDuplicate)

		result, err := svc.MarkSent(ctx, input)

		require.NoError(t, err)
		require.True(t, result.IdempotentReplay)
		require.Equal(t, MarkSentResult{IdempotentReplay: true}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
	})

	t.Run("returns error on idempotency marker create error and skips side effects", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(errors.New("idempotency create boom"))

		result, err := svc.MarkSent(ctx, input)

		require.Error(t, err)
		require.ErrorContains(t, err, "create consumer idempotency")
		require.ErrorContains(t, err, "idempotency create boom")
		require.Equal(t, MarkSentResult{}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
	})

	t.Run("maps not found", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound)

		result, err := svc.MarkSent(ctx, input)

		require.ErrorIs(t, err, ErrDeliveryRequestNotFound)
		require.Equal(t, MarkSentResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
	})

	t.Run("maps invalid arg to mark sent invalid input", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg)

		result, err := svc.MarkSent(ctx, input)

		require.ErrorIs(t, err, ErrInvalidMarkSentInput)
		require.Equal(t, MarkSentResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
	})
}

func TestMarkFailed(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	input := MarkFailedInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.delivery.results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		FailureCode:       "provider-timeout",
		FailureMessage:    "provider timeout",
		AttemptedAt:       now,
	}

	t.Run("marks failed from requested", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		da.EXPECT().Create(testifymock.Anything, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "provider",
			ProviderMessageID: "msg-1",
			FailureCode:       "provider-timeout",
			FailureMessage:    "provider timeout",
			AttemptedAt:       now,
		}).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
		dr.EXPECT().MarkFailed(testifymock.Anything, deliveryRequestID, "provider-timeout", "provider timeout").Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusFailed,
		}, nil)
		result, err := svc.MarkFailed(ctx, input)

		require.NoError(t, err)
		require.Equal(t, domain.DeliveryStatusFailed, result.DeliveryRequest.Status)
		require.False(t, result.IdempotentReplay)
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		result, err := svc.MarkFailed(ctx, MarkFailedInput{})

		require.ErrorIs(t, err, ErrInvalidMarkFailedInput)
		require.Equal(t, MarkFailedResult{}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		cid.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
	})

	t.Run("rejects invalid transition", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusSent,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)

		result, err := svc.MarkFailed(ctx, input)

		require.ErrorIs(t, err, ErrInvalidDeliveryTransition)
		require.Equal(t, MarkFailedResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("returns idempotent replay on duplicate idempotency create", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(outbound.ErrConsumerIdempotencyDuplicate)

		result, err := svc.MarkFailed(ctx, input)

		require.NoError(t, err)
		require.True(t, result.IdempotentReplay)
		require.Equal(t, MarkFailedResult{IdempotentReplay: true}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("returns error on idempotency create error and skips side effects", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(errors.New("idempotency create boom"))

		result, err := svc.MarkFailed(ctx, input)

		require.Error(t, err)
		require.ErrorContains(t, err, "create consumer idempotency")
		require.ErrorContains(t, err, "idempotency create boom")
		require.Equal(t, MarkFailedResult{}, result)
		dr.AssertNotCalled(t, "GetByID", testifymock.Anything, testifymock.Anything)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("maps not found", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound)

		result, err := svc.MarkFailed(ctx, input)

		require.ErrorIs(t, err, ErrDeliveryRequestNotFound)
		require.Equal(t, MarkFailedResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("maps invalid arg to mark failed invalid input", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.delivery.results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{}, outbound.ErrInvalidDeliveryRequestArg)

		result, err := svc.MarkFailed(ctx, input)

		require.ErrorIs(t, err, ErrInvalidMarkFailedInput)
		require.Equal(t, MarkFailedResult{}, result)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("propagates delivery attempt create error", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid)

		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(nil)
		da.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryAttempt{}, errors.New("attempt create boom"))

		result, err := svc.MarkFailed(ctx, input)

		require.Error(t, err)
		require.ErrorContains(t, err, "create delivery attempt")
		require.ErrorContains(t, err, "attempt create boom")
		require.Equal(t, MarkFailedResult{}, result)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})
}

func TestHandleOrderEvent(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	baseInput := HandleOrderEventInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		SourceEventID:     sourceEventID,
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		Body:              "order confirmed",
		AttemptedAt:       now,
	}

	t.Run("provider deterministic failure marks failed", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid).WithDeliveryProvider(deliveryProviderStub{
			sendFunc: func(context.Context, outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
				return outbound.SendDeliveryResult{
					ProviderName:      "stub-delivery",
					ProviderMessageID: "msg-fail",
					FailureCode:       "recipient_suffix_fail",
					FailureMessage:    "deterministic",
				}, nil
			},
		})

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
		dr.EXPECT().CreateRequested(testifymock.Anything, outbound.CreateDeliveryRequestInput{
			SourceEventID:   sourceEventID,
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events.delivery-results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		da.EXPECT().Create(testifymock.Anything, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "stub-delivery",
			ProviderMessageID: "msg-fail",
			FailureCode:       "recipient_suffix_fail",
			FailureMessage:    "deterministic",
			AttemptedAt:       now,
		}).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
		dr.EXPECT().MarkFailed(testifymock.Anything, deliveryRequestID, "recipient_suffix_fail", "deterministic").Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusFailed,
		}, nil)

		err := svc.HandleOrderEvent(ctx, baseInput)
		require.NoError(t, err)
	})

	t.Run("request delivery error returns and skips send", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		sendCalled := false
		svc := NewNotificationService(dr, da, cid).WithDeliveryProvider(deliveryProviderStub{
			sendFunc: func(context.Context, outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
				sendCalled = true
				return outbound.SendDeliveryResult{}, nil
			},
		})

		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
		dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{}, errors.New("request create boom"))

		err := svc.HandleOrderEvent(ctx, baseInput)
		require.Error(t, err)
		require.ErrorContains(t, err, "request delivery")
		require.ErrorContains(t, err, "request create boom")
		require.False(t, sendCalled)
	})

	t.Run("mark failed error returns", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid).WithDeliveryProvider(deliveryProviderStub{
			sendFunc: func(context.Context, outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
				return outbound.SendDeliveryResult{FailureCode: "provider_fail", FailureMessage: "provider failed"}, nil
			},
		})

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
		dr.EXPECT().CreateRequested(testifymock.Anything, outbound.CreateDeliveryRequestInput{
			SourceEventID:   sourceEventID,
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events.delivery-results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		da.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryAttempt{}, errors.New("attempt create boom"))

		err := svc.HandleOrderEvent(ctx, baseInput)
		require.Error(t, err)
		require.ErrorContains(t, err, "mark provider failure")
		require.ErrorContains(t, err, "attempt create boom")
	})

	t.Run("transient send error does not mark failed", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
		svc := NewNotificationService(dr, da, cid).WithDeliveryProvider(deliveryProviderStub{
			sendFunc: func(context.Context, outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
				return outbound.SendDeliveryResult{}, errors.New("transport timeout")
			},
		})

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
		dr.EXPECT().CreateRequested(testifymock.Anything, outbound.CreateDeliveryRequestInput{
			SourceEventID:   sourceEventID,
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
		}, nil)
		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)

		err := svc.HandleOrderEvent(ctx, baseInput)
		require.Error(t, err)
		require.ErrorContains(t, err, "send delivery")
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})
}
