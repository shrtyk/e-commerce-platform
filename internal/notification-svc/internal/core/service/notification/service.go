package notification

import (
	"errors"
	"strings"

	"github.com/shrtyk/e-commerce-platform/internal/common/tx"
	"github.com/shrtyk/e-commerce-platform/internal/notification-svc/internal/core/ports/outbound"
)

var (
	errNilDeliveryRequestsRepository      = errors.New("notification nil delivery requests repository")
	errNilDeliveryAttemptsRepository      = errors.New("notification nil delivery attempts repository")
	errNilConsumerIdempotenciesRepository = errors.New("notification nil consumer idempotencies repository")
)

type NotificationService struct {
	repos            NotificationRepos
	txProvider       tx.Provider[NotificationRepos]
	deliveryProvider outbound.DeliveryProvider
	producer         string
}

type NotificationRepos struct {
	DeliveryRequests      outbound.DeliveryRequestRepository
	DeliveryAttempts      outbound.DeliveryAttemptRepository
	ConsumerIdempotencies outbound.ConsumerIdempotencyRepository
	Publisher             outbound.EventPublisher
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
		repos: NotificationRepos{
			DeliveryRequests:      deliveryRequests,
			DeliveryAttempts:      deliveryAttempts,
			ConsumerIdempotencies: consumerIdempotencies,
		},
		producer: "notification-svc",
	}
}

func (s *NotificationService) WithDeliveryProvider(deliveryProvider outbound.DeliveryProvider) *NotificationService {
	s.deliveryProvider = deliveryProvider

	return s
}

func (s *NotificationService) WithEventPublisher(eventPublisher outbound.EventPublisher, producer string) *NotificationService {
	s.repos.Publisher = eventPublisher

	trimmedProducer := strings.TrimSpace(producer)
	if trimmedProducer != "" {
		s.producer = trimmedProducer
	}

	return s
}

func (s *NotificationService) WithTxProvider(txProvider tx.Provider[NotificationRepos]) *NotificationService {
	s.txProvider = txProvider

	return s
}
