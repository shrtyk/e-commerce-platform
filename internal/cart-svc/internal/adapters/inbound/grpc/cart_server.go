package grpc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/domain"
	"github.com/shrtyk/e-commerce-platform/internal/cart-svc/internal/core/service/cart"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cartService interface {
	GetActiveCart(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
	AddCartItem(ctx context.Context, input cart.AddCartItemInput) (domain.Cart, error)
	UpdateCartItem(ctx context.Context, input cart.UpdateCartItemInput) (domain.Cart, error)
	RemoveCartItem(ctx context.Context, input cart.RemoveCartItemInput) (domain.Cart, error)
	GetCheckoutSnapshot(ctx context.Context, userID uuid.UUID) (domain.Cart, error)
}

type CartServer struct {
	cartv1.UnimplementedCartServiceServer

	cartService cartService
	logger      *slog.Logger
}

func NewCartServer(cartService cartService, logger *slog.Logger) *CartServer {
	if logger == nil {
		logger = slog.Default()
	}

	return &CartServer{cartService: cartService, logger: logger}
}

func (s *CartServer) GetActiveCart(ctx context.Context, req *cartv1.GetActiveCartRequest) (*cartv1.GetActiveCartResponse, error) {
	requestedUserID, err := s.validateRequestedUserID(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	if s.cartService == nil {
		return nil, status.Error(grpccodes.Internal, "cart service is not configured")
	}

	result, err := s.cartService.GetActiveCart(ctx, requestedUserID)
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toGetActiveCartResponse(result), nil
}

func (s *CartServer) AddCartItem(ctx context.Context, req *cartv1.AddCartItemRequest) (*cartv1.AddCartItemResponse, error) {
	requestedUserID, err := s.validateRequestedUserID(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	if s.cartService == nil {
		return nil, status.Error(grpccodes.Internal, "cart service is not configured")
	}

	result, err := s.cartService.AddCartItem(ctx, toAddCartItemInput(requestedUserID, req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toAddCartItemResponse(result), nil
}

func (s *CartServer) UpdateCartItem(ctx context.Context, req *cartv1.UpdateCartItemRequest) (*cartv1.UpdateCartItemResponse, error) {
	requestedUserID, err := s.validateRequestedUserID(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	if s.cartService == nil {
		return nil, status.Error(grpccodes.Internal, "cart service is not configured")
	}

	result, err := s.cartService.UpdateCartItem(ctx, toUpdateCartItemInput(requestedUserID, req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toUpdateCartItemResponse(result), nil
}

func (s *CartServer) RemoveCartItem(ctx context.Context, req *cartv1.RemoveCartItemRequest) (*cartv1.RemoveCartItemResponse, error) {
	requestedUserID, err := s.validateRequestedUserID(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	if s.cartService == nil {
		return nil, status.Error(grpccodes.Internal, "cart service is not configured")
	}

	result, err := s.cartService.RemoveCartItem(ctx, toRemoveCartItemInput(requestedUserID, req))
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toRemoveCartItemResponse(result), nil
}

func (s *CartServer) GetCheckoutSnapshot(ctx context.Context, req *cartv1.GetCheckoutSnapshotRequest) (*cartv1.GetCheckoutSnapshotResponse, error) {
	requestedUserID, err := s.validateRequestedUserID(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}

	if s.cartService == nil {
		return nil, status.Error(grpccodes.Internal, "cart service is not configured")
	}

	result, err := s.cartService.GetCheckoutSnapshot(ctx, requestedUserID)
	if err != nil {
		return nil, s.mapServiceError(err)
	}

	return toGetCheckoutSnapshotResponse(result), nil
}

func (s *CartServer) validateRequestedUserID(ctx context.Context, rawUserID string) (uuid.UUID, error) {
	claims, ok := transport.ClaimsFromContext(ctx)
	if !ok {
		return uuid.Nil, status.Error(grpccodes.Unauthenticated, "missing auth claims")
	}

	requestedUserID, err := toUserID(rawUserID)
	if err != nil {
		return uuid.Nil, status.Error(grpccodes.InvalidArgument, "invalid user id")
	}

	if claims.UserID != requestedUserID {
		return uuid.Nil, status.Error(grpccodes.PermissionDenied, "request user id mismatch")
	}

	return requestedUserID, nil
}

func (s *CartServer) mapServiceError(err error) error {
	switch {
	case errors.Is(err, cart.ErrInvalidUserID),
		errors.Is(err, cart.ErrInvalidSKU),
		errors.Is(err, cart.ErrInvalidQuantity):
		return status.Error(grpccodes.InvalidArgument, "invalid cart input")
	case errors.Is(err, cart.ErrCartNotFound):
		return status.Error(grpccodes.NotFound, "cart not found")
	case errors.Is(err, cart.ErrCartItemNotFound):
		return status.Error(grpccodes.NotFound, "cart item not found")
	case errors.Is(err, cart.ErrProductSnapshotNotFound):
		return status.Error(grpccodes.NotFound, "product not found")
	case errors.Is(err, cart.ErrCartItemAlreadyExists):
		return status.Error(grpccodes.AlreadyExists, "cart item already exists")
	case errors.Is(err, cart.ErrCartCurrencyMismatch):
		return status.Error(grpccodes.FailedPrecondition, "cart currency mismatch")
	default:
		s.logger.Error("grpc internal error", "error", err.Error())
		return status.Error(grpccodes.Internal, "internal error")
	}
}
