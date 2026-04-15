package grpc

import (
	"context"
	"fmt"
	"log/slog"

	cartv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/cart/v1"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CartServer struct {
	cartv1.UnimplementedCartServiceServer

	logger *slog.Logger
}

func NewCartServer(logger *slog.Logger) *CartServer {
	return &CartServer{logger: logger}
}

func (s *CartServer) GetActiveCart(context.Context, *cartv1.GetActiveCartRequest) (*cartv1.GetActiveCartResponse, error) {
	return nil, s.unimplemented("GetActiveCart")
}

func (s *CartServer) AddCartItem(context.Context, *cartv1.AddCartItemRequest) (*cartv1.AddCartItemResponse, error) {
	return nil, s.unimplemented("AddCartItem")
}

func (s *CartServer) UpdateCartItem(context.Context, *cartv1.UpdateCartItemRequest) (*cartv1.UpdateCartItemResponse, error) {
	return nil, s.unimplemented("UpdateCartItem")
}

func (s *CartServer) RemoveCartItem(context.Context, *cartv1.RemoveCartItemRequest) (*cartv1.RemoveCartItemResponse, error) {
	return nil, s.unimplemented("RemoveCartItem")
}

func (s *CartServer) GetCheckoutSnapshot(context.Context, *cartv1.GetCheckoutSnapshotRequest) (*cartv1.GetCheckoutSnapshotResponse, error) {
	return nil, s.unimplemented("GetCheckoutSnapshot")
}

func (s *CartServer) unimplemented(method string) error {
	if s.logger != nil {
		s.logger.Debug("cart grpc method unimplemented", slog.String("method", method))
	}

	return status.Error(grpccodes.Unimplemented, fmt.Sprintf("method %s not implemented", method))
}
