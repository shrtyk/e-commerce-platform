package auth

import (
	"context"
	"strings"

	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type TokenVerifier interface {
	Verify(token string) (Claims, error)
}

type ClaimsContextSetter func(ctx context.Context, claims Claims) context.Context

func UnaryAuthInterceptor(verifier TokenVerifier, withClaims ClaimsContextSetter, publicMethods []string, requiredRoles ...Role) grpcpkg.UnaryServerInterceptor {
	public := make(map[string]struct{}, len(publicMethods))
	for _, method := range publicMethods {
		public[method] = struct{}{}
	}

	return func(ctx context.Context, req interface{}, info *grpcpkg.UnaryServerInfo, handler grpcpkg.UnaryHandler) (interface{}, error) {
		if _, ok := public[info.FullMethod]; ok {
			return handler(ctx, req)
		}

		token, ok := bearerTokenFromMetadata(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing or malformed authorization header")
		}

		if verifier == nil {
			return nil, status.Error(codes.Unauthenticated, "token verifier is not configured")
		}

		claims, err := verifier.Verify(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		if err := claims.Validate(); err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid claims")
		}

		if !claims.CanAccess(requiredRoles...) {
			return nil, status.Error(codes.PermissionDenied, "insufficient permissions")
		}

		if withClaims != nil {
			ctx = withClaims(ctx, claims)
		}

		return handler(ctx, req)
	}
}

func bearerTokenFromMetadata(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}

	headers := md.Get("authorization")
	if len(headers) == 0 {
		return "", false
	}

	authorization := strings.TrimSpace(headers[0])
	scheme, token, ok := strings.Cut(authorization, " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}

	return token, true
}
