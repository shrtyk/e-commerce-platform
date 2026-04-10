package http

import (
	"net/http"
	"strings"

	"github.com/shrtyk/e-commerce-platform/internal/common/transport"
)

type TokenVerifier interface {
	Verify(token string) (transport.Claims, error)
}

func (p *MiddlewaresProvider) Auth(requiredRoles ...string) func(http.Handler) http.Handler {
	allowedRoles := make(map[string]struct{}, len(requiredRoles))
	for _, role := range requiredRoles {
		allowedRoles[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p.tokenVerifier == nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			scheme, token, ok := strings.Cut(authHeader, " ")
			token = strings.TrimSpace(token)
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			claims, err := p.tokenVerifier.Verify(token)
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if len(allowedRoles) > 0 {
				if _, allowed := allowedRoles[claims.Role]; !allowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(transport.WithClaims(r.Context(), claims)))
		})
	}
}
