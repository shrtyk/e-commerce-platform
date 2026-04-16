package grpc

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonauth "github.com/shrtyk/e-commerce-platform/internal/common/auth"
	orderv1 "github.com/shrtyk/e-commerce-platform/internal/common/gen/proto/order/v1"
	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
	grpccommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/grpc"
	httpcommon "github.com/shrtyk/e-commerce-platform/internal/common/transport/http"
)

var publicMethods []string

func NewServer(
	logger *slog.Logger,
	serviceName string,
	tokenVerifier httpcommon.TokenVerifier,
	tracer trace.Tracer,
) *grpcpkg.Server {
	if tokenVerifier == nil {
		panic("grpc token verifier is required")
	}

	interceptorsProvider := grpccommon.NewInterceptorsProviderWithTracer(serviceName, logger, tracer)

	server := grpcpkg.NewServer(
		grpcpkg.ChainUnaryInterceptor(
			interceptorsProvider.UnaryTracing(),
			interceptorsProvider.UnaryLogging(),
			interceptorsProvider.UnaryRecovery(),
			commonauth.UnaryAuthInterceptor(
				newGRPCTokenVerifier(tokenVerifier),
				func(ctx context.Context, claims commonauth.Claims) context.Context {
					return transport.WithClaims(ctx, transport.Claims{
						UserID: claims.UserID,
						Role:   string(claims.Role),
						Status: string(claims.Status),
					})
				},
				publicMethods,
			),
		),
	)

	orderv1.RegisterOrderServiceServer(server, NewOrderServer())

	return server
}

type OrderServer struct {
	orderv1.UnimplementedOrderServiceServer
}

func NewOrderServer() *OrderServer {
	return &OrderServer{}
}

func (s *OrderServer) CreateOrder(context.Context, *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method CreateOrder not implemented")
}

func (s *OrderServer) GetOrder(context.Context, *orderv1.GetOrderRequest) (*orderv1.GetOrderResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method GetOrder not implemented")
}

type grpcTokenVerifier struct {
	tokenVerifier httpcommon.TokenVerifier
}

func newGRPCTokenVerifier(tokenVerifier httpcommon.TokenVerifier) commonauth.TokenVerifier {
	return grpcTokenVerifier{tokenVerifier: tokenVerifier}
}

func (v grpcTokenVerifier) Verify(token string) (commonauth.Claims, error) {
	claims, err := v.tokenVerifier.Verify(token)
	if err != nil {
		return commonauth.Claims{}, err
	}

	return transport.ToAuthClaims(claims)
}
