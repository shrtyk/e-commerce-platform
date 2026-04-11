package outbound

import (
	"context"

	"github.com/shrtyk/e-commerce-platform/internal/product-svc/internal/core/domain"
)

//mockery:generate: true
type EventPublisher interface {
	Publish(ctx context.Context, event domain.DomainEvent) error
}
