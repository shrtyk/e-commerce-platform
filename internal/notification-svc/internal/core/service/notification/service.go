package notification

import (
	"errors"

	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

var (
	errNilDeliveryRequestsRepository      = errors.New("notification nil delivery requests repository")
	errNilDeliveryAttemptsRepository      = errors.New("notification nil delivery attempts repository")
	errNilConsumerIdempotenciesRepository = errors.New("notification nil consumer idempotencies repository")
)

type NotificationService struct {
	deliveryRequests      outbound.DeliveryRequestRepository
	deliveryAttempts      outbound.DeliveryAttemptRepository
	consumerIdempotencies outbound.ConsumerIdempotencyRepository
}

func NewNotificationService(
	deliveryRequests outbound.DeliveryRequestRepository,
	deliveryAttempts outbound.DeliveryAttemptRepository,
	consumerIdempotencies outbound.ConsumerIdempotencyRepository,
) *NotificationService {
	if deliveryRequests == nil {
		panic(errNilDeliveryRequestsRepository)
	}
	if deliveryAttempts == nil {
		panic(errNilDeliveryAttemptsRepository)
	}
	if consumerIdempotencies == nil {
		panic(errNilConsumerIdempotenciesRepository)
	}

	return &NotificationService{
		deliveryRequests:      deliveryRequests,
		deliveryAttempts:      deliveryAttempts,
		consumerIdempotencies: consumerIdempotencies,
	}
}
