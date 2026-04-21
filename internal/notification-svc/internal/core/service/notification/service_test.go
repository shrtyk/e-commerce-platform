package notification

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	commontx "github.com/shrtyk/e-commerce-platform/internal/common/tx"
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

type txUnitOfWorkStub struct {
	repos NotificationRepos
}

func (u txUnitOfWorkStub) Repos() NotificationRepos {
	return u.repos
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
		CorrelationID:     "corr-order-created",
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
					CorrelationID:   "corr-order-created",
					SourceEventName: "order.created",
					Channel:         "email",
					Recipient:       "user@example.com",
					TemplateKey:     "order-created",
					IdempotencyKey:  "req-order-1",
				}).Return(domain.DeliveryRequest{
					DeliveryRequestID: deliveryRequestID,
					SourceEventID:     eventID,
					CorrelationID:     "corr-order-created",
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
					CorrelationID:     "corr-order-created",
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
					CorrelationID:     "corr-order-created",
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
					CorrelationID:     "corr-order-created",
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
					CorrelationID:     "corr-order-created",
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
		CorrelationID:     "corr-order-events",
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
			CorrelationID:   "corr-order-events",
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
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
			CorrelationID:     "corr-order-events",
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
			CorrelationID:     "corr-order-events",
			Status:            domain.DeliveryStatusFailed,
		}, nil)

		err := svc.HandleOrderEvent(ctx, baseInput)
		require.NoError(t, err)
	})

	t.Run("idempotent replay with non-requested status skips provider send", func(t *testing.T) {
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

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(true, nil)
		dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, idempotencyKey).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusSent,
			IdempotencyKey:    idempotencyKey,
		}, nil)

		err := svc.HandleOrderEvent(ctx, baseInput)

		require.NoError(t, err)
		require.False(t, sendCalled)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("idempotent replay with failed status skips provider send", func(t *testing.T) {
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

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(true, nil)
		dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, idempotencyKey).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			Status:            domain.DeliveryStatusFailed,
			IdempotencyKey:    idempotencyKey,
		}, nil)

		err := svc.HandleOrderEvent(ctx, baseInput)

		require.NoError(t, err)
		require.False(t, sendCalled)
		da.AssertNotCalled(t, "Create", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkSent", testifymock.Anything, testifymock.Anything)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
	})

	t.Run("idempotent replay with requested status retries provider send", func(t *testing.T) {
		dr := outboundmocks.NewMockDeliveryRequestRepository(t)
		da := outboundmocks.NewMockDeliveryAttemptRepository(t)
		cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)

		sendCalled := false
		svc := NewNotificationService(dr, da, cid).WithDeliveryProvider(deliveryProviderStub{
			sendFunc: func(_ context.Context, input outbound.SendDeliveryInput) (outbound.SendDeliveryResult, error) {
				sendCalled = true
				require.Equal(t, deliveryRequestID, input.DeliveryRequestID)
				require.Equal(t, "in_app", input.Channel)
				require.Equal(t, "user-1", input.Recipient)
				require.Equal(t, "order-confirmed", input.TemplateKey)
				require.Equal(t, "order confirmed", input.Body)

				return outbound.SendDeliveryResult{ProviderName: "provider", ProviderMessageID: "msg-1"}, nil
			},
		})

		idempotencyKey := "order.confirmed:" + eventID.String()
		cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(true, nil)
		dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, idempotencyKey).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
			SourceEventName:   "order.confirmed",
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
			IdempotencyKey:    idempotencyKey,
		}, nil)

		cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
			EventID:           eventID,
			ConsumerGroupName: "notification.order.events.delivery-results",
			DeliveryRequestID: deliveryRequestID,
		}).Return(nil)
		dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
			SourceEventName:   "order.confirmed",
			Channel:           "in_app",
			Recipient:         "user-1",
			TemplateKey:       "order-confirmed",
			Status:            domain.DeliveryStatusRequested,
			IdempotencyKey:    idempotencyKey,
		}, nil)
		da.EXPECT().Create(testifymock.Anything, outbound.CreateDeliveryAttemptInput{
			DeliveryRequestID: deliveryRequestID,
			AttemptNumber:     1,
			ProviderName:      "provider",
			ProviderMessageID: "msg-1",
			AttemptedAt:       now,
		}).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
		dr.EXPECT().MarkSent(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
			SourceEventName:   "order.confirmed",
			Channel:           "in_app",
			Recipient:         "user-1",
			Status:            domain.DeliveryStatusSent,
			IdempotencyKey:    idempotencyKey,
		}, nil)

		err := svc.HandleOrderEvent(ctx, baseInput)

		require.NoError(t, err)
		require.True(t, sendCalled)
		dr.AssertNotCalled(t, "MarkFailed", testifymock.Anything, testifymock.Anything, testifymock.Anything, testifymock.Anything)
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
			CorrelationID:   "corr-order-events",
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
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
			CorrelationID:     "corr-order-events",
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
			CorrelationID:   "corr-order-events",
			SourceEventName: "order.confirmed",
			Channel:         "in_app",
			Recipient:       "user-1",
			TemplateKey:     "order-confirmed",
			IdempotencyKey:  idempotencyKey,
		}).Return(domain.DeliveryRequest{
			DeliveryRequestID: deliveryRequestID,
			CorrelationID:     "corr-order-events",
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

func TestRequestDeliveryPublishesRequestedEvent(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	input := RequestDeliveryInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		SourceEventID:     sourceEventID,
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		IdempotencyKey:    "order.confirmed:" + eventID.String(),
	}

	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
	dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     sourceEventID,
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    input.IdempotencyKey,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil)
	cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		DeliveryRequestID: deliveryRequestID,
	}).Return(nil)
	publisher.EXPECT().Publish(testifymock.Anything, testifymock.MatchedBy(func(event domain.DomainEvent) bool {
		if event.EventName != "notification.delivery_requested" || event.Topic != "notification.events" || event.AggregateID != deliveryRequestID.String() {
			return false
		}
		if event.CorrelationID != "corr-order-confirmed" || event.CausationID != eventID.String() {
			return false
		}

		payload, ok := event.Payload.(domain.DeliveryRequestedPayload)
		if !ok {
			return false
		}

		return payload.DeliveryRequestID == deliveryRequestID.String() && payload.Status == domain.DeliveryStatusRequested
	})).Return(nil)

	result, err := svc.RequestDelivery(ctx, input)
	require.NoError(t, err)
	require.False(t, result.IdempotentReplay)
}

func TestRequestDeliveryPublishesRequestedEventUsesStoredCorrelationID(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	sourceEventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	input := RequestDeliveryInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		SourceEventID:     sourceEventID,
		CorrelationID:     "corr-from-input",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		IdempotencyKey:    "order.confirmed:" + eventID.String(),
	}

	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(false, nil)
	dr.EXPECT().CreateRequested(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     sourceEventID,
		CorrelationID:     "corr-from-store",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    input.IdempotencyKey,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil)
	cid.EXPECT().Create(testifymock.Anything, outbound.CreateConsumerIdempotencyInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		DeliveryRequestID: deliveryRequestID,
	}).Return(nil)
	publisher.EXPECT().Publish(testifymock.Anything, testifymock.MatchedBy(func(event domain.DomainEvent) bool {
		return event.EventName == "notification.delivery_requested" &&
			event.CorrelationID == "corr-from-store" &&
			event.CorrelationID != input.CorrelationID
	})).Return(nil)

	result, err := svc.RequestDelivery(ctx, input)
	require.NoError(t, err)
	require.False(t, result.IdempotentReplay)
}

func TestRequestDeliveryReplaySkipsPublish(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()

	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Exists(testifymock.Anything, eventID, "notification.order.events").Return(true, nil)
	dr.EXPECT().GetByIdempotencyKey(testifymock.Anything, "order.confirmed:"+eventID.String()).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		Status:            domain.DeliveryStatusRequested,
	}, nil)

	_, err := svc.RequestDelivery(ctx, RequestDeliveryInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		IdempotencyKey:    "order.confirmed:" + eventID.String(),
	})
	require.NoError(t, err)
	publisher.AssertNotCalled(t, "Publish", testifymock.Anything, testifymock.Anything)
}

func TestMarkSentPublishesEvent(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(nil)
	dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    "idem-1",
	}, nil)
	da.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
	dr.EXPECT().MarkSent(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		Status:            domain.DeliveryStatusSent,
		IdempotencyKey:    "idem-1",
	}, nil)
	publisher.EXPECT().Publish(testifymock.Anything, testifymock.MatchedBy(func(event domain.DomainEvent) bool {
		if event.EventName != "notification.sent" {
			return false
		}
		if event.Topic != "notification.events" || event.AggregateID != deliveryRequestID.String() {
			return false
		}
		if event.CorrelationID != "corr-order-confirmed" || event.CausationID != eventID.String() {
			return false
		}
		if event.Producer != "notification-svc" || event.SchemaVersion != "1" {
			return false
		}
		if event.Headers["idempotencyKey"] != "idem-1" {
			return false
		}
		payload, ok := event.Payload.(domain.NotificationSentPayload)
		return ok && payload.DeliveryRequestID == deliveryRequestID.String() && payload.Status == domain.DeliveryStatusSent
	})).Return(nil)

	_, err := svc.MarkSent(ctx, MarkSentInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		AttemptedAt:       now,
	})
	require.NoError(t, err)
}

func TestMarkFailedReplaySkipsPublish(t *testing.T) {
	ctx := context.Background()
	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(outbound.ErrConsumerIdempotencyDuplicate)

	result, err := svc.MarkFailed(ctx, MarkFailedInput{
		EventID:           uuid.New(),
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: uuid.New(),
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		FailureCode:       "provider-timeout",
		FailureMessage:    "provider timeout",
		AttemptedAt:       time.Now().UTC(),
	})
	require.NoError(t, err)
	require.True(t, result.IdempotentReplay)
	publisher.AssertNotCalled(t, "Publish", testifymock.Anything, testifymock.Anything)
}

func TestMarkFailedPublishesEvent(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	dr := outboundmocks.NewMockDeliveryRequestRepository(t)
	da := outboundmocks.NewMockDeliveryAttemptRepository(t)
	cid := outboundmocks.NewMockConsumerIdempotencyRepository(t)
	publisher := outboundmocks.NewMockEventPublisher(t)

	svc := NewNotificationService(dr, da, cid).WithEventPublisher(publisher, "notification-svc")

	cid.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(nil)
	dr.EXPECT().GetByID(testifymock.Anything, deliveryRequestID).Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-cancelled",
		SourceEventName:   "order.cancelled",
		Channel:           "in_app",
		Recipient:         "user-1",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    "idem-failed-1",
	}, nil)
	da.EXPECT().Create(testifymock.Anything, testifymock.Anything).Return(domain.DeliveryAttempt{DeliveryRequestID: deliveryRequestID, AttemptNumber: 1}, nil)
	dr.EXPECT().MarkFailed(testifymock.Anything, deliveryRequestID, "provider-timeout", "provider timeout").Return(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-cancelled",
		SourceEventName:   "order.cancelled",
		Channel:           "in_app",
		Recipient:         "user-1",
		Status:            domain.DeliveryStatusFailed,
		IdempotencyKey:    "idem-failed-1",
		LastErrorCode:     "provider-timeout",
		LastErrorMessage:  "provider timeout",
	}, nil)
	publisher.EXPECT().Publish(testifymock.Anything, testifymock.MatchedBy(func(event domain.DomainEvent) bool {
		if event.EventName != "notification.failed" || event.Topic != "notification.events" || event.AggregateID != deliveryRequestID.String() {
			return false
		}
		if event.CorrelationID != "corr-order-cancelled" || event.CausationID != eventID.String() {
			return false
		}
		if event.Producer != "notification-svc" || event.SchemaVersion != "1" {
			return false
		}
		if event.Headers["idempotencyKey"] != "idem-failed-1" {
			return false
		}
		payload, ok := event.Payload.(domain.NotificationFailedPayload)
		if !ok {
			return false
		}

		return payload.DeliveryRequestID == deliveryRequestID.String() &&
			payload.Status == domain.DeliveryStatusFailed &&
			payload.FailureCode == "provider-timeout" &&
			payload.FailureMessage == "provider timeout"
	})).Return(nil)

	_, err := svc.MarkFailed(ctx, MarkFailedInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		FailureCode:       "provider-timeout",
		FailureMessage:    "provider timeout",
		AttemptedAt:       now,
	})
	require.NoError(t, err)
}

func TestRequestDeliveryRollsBackWhenPublishFails(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	sourceEventID := uuid.New()
	repos := newTxAssertionRepos()
	repos.publisher.err = errors.New("publish boom")

	svc := NewNotificationService(repos.deliveryRequests, repos.deliveryAttempts, repos.consumerIdempotencies).
		WithEventPublisher(repos.publisher, "notification-svc").
		WithTxProvider(repos.txProvider())

	input := RequestDeliveryInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events",
		SourceEventID:     sourceEventID,
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		IdempotencyKey:    "order.confirmed:" + eventID.String(),
	}

	result, err := svc.RequestDelivery(ctx, input)

	require.ErrorContains(t, err, "publish delivery requested event")
	require.ErrorContains(t, err, "publish boom")
	require.Equal(t, RequestDeliveryResult{}, result)
	require.True(t, repos.tx.rolledBack)
	require.False(t, repos.tx.committed)
	require.Equal(t, 1, repos.tx.withTxCalls)
	require.Empty(t, repos.deliveryRequests.byID)
	require.Empty(t, repos.deliveryRequests.byIdempotencyKey)
	require.Empty(t, repos.consumerIdempotencies.byKey)

	repos.publisher.err = nil

	secondResult, secondErr := svc.RequestDelivery(ctx, input)
	require.NoError(t, secondErr)
	require.False(t, secondResult.IdempotentReplay)
	require.Equal(t, 1, len(repos.deliveryRequests.byID))
	require.Equal(t, 1, len(repos.deliveryRequests.byIdempotencyKey))
	require.Equal(t, 1, len(repos.consumerIdempotencies.byKey))
}

func TestMarkSentRollsBackWhenPublishFails(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	repos := newTxLifecycleRepos(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-confirmed",
		SourceEventName:   "order.confirmed",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-confirmed",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    "idem-sent-rollback",
	})
	repos.publisher.err = errors.New("publish boom")

	svc := NewNotificationService(repos.deliveryRequests, repos.deliveryAttempts, repos.consumerIdempotencies).
		WithEventPublisher(repos.publisher, "notification-svc").
		WithTxProvider(repos.txProvider())

	result, err := svc.MarkSent(ctx, MarkSentInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		AttemptedAt:       now,
	})

	require.ErrorContains(t, err, "publish notification sent event")
	require.ErrorContains(t, err, "publish boom")
	require.Equal(t, MarkSentResult{}, result)
	require.True(t, repos.tx.rolledBack)
	require.False(t, repos.tx.committed)
	require.Equal(t, 1, repos.tx.withTxCalls)
	require.Equal(t, domain.DeliveryStatusRequested, repos.deliveryRequests.byID[deliveryRequestID].Status)
	require.Empty(t, repos.deliveryAttempts.byRequestID[deliveryRequestID])
	require.Empty(t, repos.consumerIdempotencies.byKey)

	repos.publisher.err = nil

	secondResult, secondErr := svc.MarkSent(ctx, MarkSentInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		AttemptedAt:       now,
	})
	require.NoError(t, secondErr)
	require.False(t, secondResult.IdempotentReplay)
	require.Equal(t, 2, repos.tx.withTxCalls)
	require.True(t, repos.tx.committed)
	require.Equal(t, domain.DeliveryStatusSent, repos.deliveryRequests.byID[deliveryRequestID].Status)
	require.Len(t, repos.deliveryAttempts.byRequestID[deliveryRequestID], 1)
	require.Len(t, repos.consumerIdempotencies.byKey, 1)
}

func TestMarkFailedRollsBackWhenPublishFails(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New()
	deliveryRequestID := uuid.New()
	now := time.Now().UTC()

	repos := newTxLifecycleRepos(domain.DeliveryRequest{
		DeliveryRequestID: deliveryRequestID,
		SourceEventID:     uuid.New(),
		CorrelationID:     "corr-order-cancelled",
		SourceEventName:   "order.cancelled",
		Channel:           "in_app",
		Recipient:         "user-1",
		TemplateKey:       "order-cancelled",
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    "idem-failed-rollback",
	})
	repos.publisher.err = errors.New("publish boom")

	svc := NewNotificationService(repos.deliveryRequests, repos.deliveryAttempts, repos.consumerIdempotencies).
		WithEventPublisher(repos.publisher, "notification-svc").
		WithTxProvider(repos.txProvider())

	result, err := svc.MarkFailed(ctx, MarkFailedInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		FailureCode:       "provider-timeout",
		FailureMessage:    "provider timeout",
		AttemptedAt:       now,
	})

	require.ErrorContains(t, err, "publish notification failed event")
	require.ErrorContains(t, err, "publish boom")
	require.Equal(t, MarkFailedResult{}, result)
	require.True(t, repos.tx.rolledBack)
	require.False(t, repos.tx.committed)
	require.Equal(t, 1, repos.tx.withTxCalls)
	require.Equal(t, domain.DeliveryStatusRequested, repos.deliveryRequests.byID[deliveryRequestID].Status)
	require.Empty(t, repos.deliveryAttempts.byRequestID[deliveryRequestID])
	require.Empty(t, repos.consumerIdempotencies.byKey)

	repos.publisher.err = nil

	secondResult, secondErr := svc.MarkFailed(ctx, MarkFailedInput{
		EventID:           eventID,
		ConsumerGroupName: "notification.order.events.delivery-results",
		DeliveryRequestID: deliveryRequestID,
		AttemptNumber:     1,
		ProviderName:      "provider",
		ProviderMessageID: "msg-1",
		FailureCode:       "provider-timeout",
		FailureMessage:    "provider timeout",
		AttemptedAt:       now,
	})
	require.NoError(t, secondErr)
	require.False(t, secondResult.IdempotentReplay)
	require.Equal(t, 2, repos.tx.withTxCalls)
	require.True(t, repos.tx.committed)
	require.Equal(t, domain.DeliveryStatusFailed, repos.deliveryRequests.byID[deliveryRequestID].Status)
	require.Len(t, repos.deliveryAttempts.byRequestID[deliveryRequestID], 1)
	require.Len(t, repos.consumerIdempotencies.byKey, 1)
}

type txLifecycleRepos struct {
	deliveryRequests      *txLifecycleDeliveryRequestRepo
	deliveryAttempts      *txLifecycleDeliveryAttemptRepo
	consumerIdempotencies *txAssertionConsumerIdempotencyRepo
	publisher             *txAssertionPublisher
	tx                    *txLifecycleProvider
}

func newTxLifecycleRepos(initial domain.DeliveryRequest) *txLifecycleRepos {
	deliveryRequests := &txLifecycleDeliveryRequestRepo{
		byID:             map[uuid.UUID]domain.DeliveryRequest{initial.DeliveryRequestID: initial},
		byIdempotencyKey: map[string]uuid.UUID{initial.IdempotencyKey: initial.DeliveryRequestID},
	}

	repos := &txLifecycleRepos{
		deliveryRequests: deliveryRequests,
		deliveryAttempts: &txLifecycleDeliveryAttemptRepo{byRequestID: map[uuid.UUID][]domain.DeliveryAttempt{}},
		consumerIdempotencies: &txAssertionConsumerIdempotencyRepo{
			byKey: map[string]outbound.CreateConsumerIdempotencyInput{},
		},
		publisher: &txAssertionPublisher{},
	}

	repos.tx = &txLifecycleProvider{repos: repos}

	return repos
}

func (r *txLifecycleRepos) txProvider() *txLifecycleProvider {
	return r.tx
}

func (r *txLifecycleRepos) clone() *txLifecycleRepos {
	deliveryRequests := &txLifecycleDeliveryRequestRepo{byID: map[uuid.UUID]domain.DeliveryRequest{}, byIdempotencyKey: map[string]uuid.UUID{}}
	for id, request := range r.deliveryRequests.byID {
		deliveryRequests.byID[id] = request
	}
	for key, id := range r.deliveryRequests.byIdempotencyKey {
		deliveryRequests.byIdempotencyKey[key] = id
	}

	deliveryAttempts := &txLifecycleDeliveryAttemptRepo{byRequestID: map[uuid.UUID][]domain.DeliveryAttempt{}}
	for requestID, attempts := range r.deliveryAttempts.byRequestID {
		copied := make([]domain.DeliveryAttempt, len(attempts))
		copy(copied, attempts)
		deliveryAttempts.byRequestID[requestID] = copied
	}

	consumerIdempotencies := &txAssertionConsumerIdempotencyRepo{byKey: map[string]outbound.CreateConsumerIdempotencyInput{}}
	for key, input := range r.consumerIdempotencies.byKey {
		consumerIdempotencies.byKey[key] = input
	}

	return &txLifecycleRepos{
		deliveryRequests:      deliveryRequests,
		deliveryAttempts:      deliveryAttempts,
		consumerIdempotencies: consumerIdempotencies,
		publisher:             r.publisher,
	}
}

func (r *txLifecycleRepos) applyFrom(clone *txLifecycleRepos) {
	r.deliveryRequests = clone.deliveryRequests
	r.deliveryAttempts = clone.deliveryAttempts
	r.consumerIdempotencies = clone.consumerIdempotencies
}

type txLifecycleProvider struct {
	repos       *txLifecycleRepos
	committed   bool
	rolledBack  bool
	withTxCalls int
}

func (p *txLifecycleProvider) WithTransaction(ctx context.Context, _ *sql.TxOptions, fn func(commontx.UnitOfWork[NotificationRepos]) error) error {
	p.withTxCalls++
	clone := p.repos.clone()
	err := fn(txUnitOfWorkStub{repos: NotificationRepos{
		DeliveryRequests:      clone.deliveryRequests,
		DeliveryAttempts:      clone.deliveryAttempts,
		ConsumerIdempotencies: clone.consumerIdempotencies,
		Publisher:             clone.publisher,
	}})
	if err != nil {
		p.rolledBack = true
		return err
	}

	p.repos.applyFrom(clone)
	p.committed = true

	return nil
}

type txLifecycleDeliveryRequestRepo struct {
	byID             map[uuid.UUID]domain.DeliveryRequest
	byIdempotencyKey map[string]uuid.UUID
}

func (r *txLifecycleDeliveryRequestRepo) CreateRequested(_ context.Context, _ outbound.CreateDeliveryRequestInput) (domain.DeliveryRequest, error) {
	return domain.DeliveryRequest{}, errors.New("unexpected CreateRequested call")
}

func (r *txLifecycleDeliveryRequestRepo) GetByID(_ context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error) {
	request, ok := r.byID[deliveryRequestID]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	return request, nil
}

func (r *txLifecycleDeliveryRequestRepo) GetByIdempotencyKey(_ context.Context, idempotencyKey string) (domain.DeliveryRequest, error) {
	id, ok := r.byIdempotencyKey[idempotencyKey]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	request, ok := r.byID[id]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	return request, nil
}

func (r *txLifecycleDeliveryRequestRepo) MarkSent(_ context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error) {
	request, ok := r.byID[deliveryRequestID]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	request.Status = domain.DeliveryStatusSent
	r.byID[deliveryRequestID] = request

	return request, nil
}

func (r *txLifecycleDeliveryRequestRepo) MarkFailed(_ context.Context, deliveryRequestID uuid.UUID, failureCode string, failureMessage string) (domain.DeliveryRequest, error) {
	request, ok := r.byID[deliveryRequestID]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	request.Status = domain.DeliveryStatusFailed
	request.LastErrorCode = failureCode
	request.LastErrorMessage = failureMessage
	r.byID[deliveryRequestID] = request

	return request, nil
}

type txLifecycleDeliveryAttemptRepo struct {
	byRequestID map[uuid.UUID][]domain.DeliveryAttempt
}

func (r *txLifecycleDeliveryAttemptRepo) Create(_ context.Context, input outbound.CreateDeliveryAttemptInput) (domain.DeliveryAttempt, error) {
	attempt := domain.DeliveryAttempt{
		DeliveryRequestID: input.DeliveryRequestID,
		AttemptNumber:     input.AttemptNumber,
		ProviderName:      input.ProviderName,
		ProviderMessageID: input.ProviderMessageID,
		FailureCode:       input.FailureCode,
		FailureMessage:    input.FailureMessage,
		AttemptedAt:       input.AttemptedAt,
	}

	r.byRequestID[input.DeliveryRequestID] = append(r.byRequestID[input.DeliveryRequestID], attempt)

	return attempt, nil
}

func (r *txLifecycleDeliveryAttemptRepo) ListByDeliveryRequestID(_ context.Context, deliveryRequestID uuid.UUID) ([]domain.DeliveryAttempt, error) {
	attempts := r.byRequestID[deliveryRequestID]
	result := make([]domain.DeliveryAttempt, len(attempts))
	copy(result, attempts)

	return result, nil
}

type txAssertionRepos struct {
	deliveryRequests      *txAssertionDeliveryRequestRepo
	deliveryAttempts      *txAssertionDeliveryAttemptRepo
	consumerIdempotencies *txAssertionConsumerIdempotencyRepo
	publisher             *txAssertionPublisher
	tx                    *txAssertionProvider
}

func newTxAssertionRepos() *txAssertionRepos {
	repos := &txAssertionRepos{
		deliveryRequests:      &txAssertionDeliveryRequestRepo{byID: map[uuid.UUID]domain.DeliveryRequest{}, byIdempotencyKey: map[string]uuid.UUID{}},
		deliveryAttempts:      &txAssertionDeliveryAttemptRepo{},
		consumerIdempotencies: &txAssertionConsumerIdempotencyRepo{byKey: map[string]outbound.CreateConsumerIdempotencyInput{}},
		publisher:             &txAssertionPublisher{},
	}

	repos.tx = &txAssertionProvider{repos: repos}

	return repos
}

func (r *txAssertionRepos) txProvider() *txAssertionProvider {
	return r.tx
}

func (r *txAssertionRepos) clone() *txAssertionRepos {
	deliveryRequests := &txAssertionDeliveryRequestRepo{byID: map[uuid.UUID]domain.DeliveryRequest{}, byIdempotencyKey: map[string]uuid.UUID{}}
	for id, request := range r.deliveryRequests.byID {
		deliveryRequests.byID[id] = request
	}
	for key, id := range r.deliveryRequests.byIdempotencyKey {
		deliveryRequests.byIdempotencyKey[key] = id
	}

	consumerIdempotencies := &txAssertionConsumerIdempotencyRepo{byKey: map[string]outbound.CreateConsumerIdempotencyInput{}}
	for key, input := range r.consumerIdempotencies.byKey {
		consumerIdempotencies.byKey[key] = input
	}

	return &txAssertionRepos{
		deliveryRequests:      deliveryRequests,
		deliveryAttempts:      &txAssertionDeliveryAttemptRepo{},
		consumerIdempotencies: consumerIdempotencies,
		publisher:             r.publisher,
	}
}

func (r *txAssertionRepos) applyFrom(clone *txAssertionRepos) {
	r.deliveryRequests = clone.deliveryRequests
	r.consumerIdempotencies = clone.consumerIdempotencies
}

type txAssertionProvider struct {
	repos       *txAssertionRepos
	committed   bool
	rolledBack  bool
	withTxCalls int
}

func (p *txAssertionProvider) WithTransaction(ctx context.Context, _ *sql.TxOptions, fn func(commontx.UnitOfWork[NotificationRepos]) error) error {
	p.withTxCalls++
	clone := p.repos.clone()
	err := fn(txUnitOfWorkStub{repos: NotificationRepos{
		DeliveryRequests:      clone.deliveryRequests,
		DeliveryAttempts:      clone.deliveryAttempts,
		ConsumerIdempotencies: clone.consumerIdempotencies,
		Publisher:             clone.publisher,
	}})
	if err != nil {
		p.rolledBack = true
		return err
	}

	p.repos.applyFrom(clone)
	p.committed = true

	return nil
}

type txAssertionDeliveryRequestRepo struct {
	byID             map[uuid.UUID]domain.DeliveryRequest
	byIdempotencyKey map[string]uuid.UUID
}

func (r *txAssertionDeliveryRequestRepo) CreateRequested(_ context.Context, input outbound.CreateDeliveryRequestInput) (domain.DeliveryRequest, error) {
	id := uuid.New()
	request := domain.DeliveryRequest{
		DeliveryRequestID: id,
		SourceEventID:     input.SourceEventID,
		CorrelationID:     input.CorrelationID,
		SourceEventName:   input.SourceEventName,
		Channel:           input.Channel,
		Recipient:         input.Recipient,
		TemplateKey:       input.TemplateKey,
		Status:            domain.DeliveryStatusRequested,
		IdempotencyKey:    input.IdempotencyKey,
	}

	r.byID[id] = request
	r.byIdempotencyKey[input.IdempotencyKey] = id

	return request, nil
}

func (r *txAssertionDeliveryRequestRepo) GetByID(_ context.Context, deliveryRequestID uuid.UUID) (domain.DeliveryRequest, error) {
	request, ok := r.byID[deliveryRequestID]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	return request, nil
}

func (r *txAssertionDeliveryRequestRepo) GetByIdempotencyKey(_ context.Context, idempotencyKey string) (domain.DeliveryRequest, error) {
	id, ok := r.byIdempotencyKey[idempotencyKey]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	request, ok := r.byID[id]
	if !ok {
		return domain.DeliveryRequest{}, outbound.ErrDeliveryRequestNotFound
	}

	return request, nil
}

func (r *txAssertionDeliveryRequestRepo) MarkSent(_ context.Context, _ uuid.UUID) (domain.DeliveryRequest, error) {
	return domain.DeliveryRequest{}, errors.New("unexpected MarkSent call")
}

func (r *txAssertionDeliveryRequestRepo) MarkFailed(_ context.Context, _ uuid.UUID, _, _ string) (domain.DeliveryRequest, error) {
	return domain.DeliveryRequest{}, errors.New("unexpected MarkFailed call")
}

type txAssertionDeliveryAttemptRepo struct{}

func (r *txAssertionDeliveryAttemptRepo) Create(_ context.Context, _ outbound.CreateDeliveryAttemptInput) (domain.DeliveryAttempt, error) {
	return domain.DeliveryAttempt{}, errors.New("unexpected Create call")
}

func (r *txAssertionDeliveryAttemptRepo) ListByDeliveryRequestID(_ context.Context, _ uuid.UUID) ([]domain.DeliveryAttempt, error) {
	return nil, errors.New("unexpected ListByDeliveryRequestID call")
}

type txAssertionConsumerIdempotencyRepo struct {
	byKey map[string]outbound.CreateConsumerIdempotencyInput
}

func (r *txAssertionConsumerIdempotencyRepo) Create(_ context.Context, input outbound.CreateConsumerIdempotencyInput) error {
	key := txAssertionIdempotencyKey(input.EventID, input.ConsumerGroupName)
	if _, exists := r.byKey[key]; exists {
		return outbound.ErrConsumerIdempotencyDuplicate
	}

	r.byKey[key] = input

	return nil
}

func (r *txAssertionConsumerIdempotencyRepo) Exists(_ context.Context, eventID uuid.UUID, consumerGroupName string) (bool, error) {
	_, exists := r.byKey[txAssertionIdempotencyKey(eventID, consumerGroupName)]

	return exists, nil
}

type txAssertionPublisher struct {
	err error
}

func (p *txAssertionPublisher) Publish(_ context.Context, _ domain.DomainEvent) error {
	return p.err
}

func txAssertionIdempotencyKey(eventID uuid.UUID, consumerGroupName string) string {
	return eventID.String() + ":" + consumerGroupName
}
