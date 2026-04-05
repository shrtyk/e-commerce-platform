package outbound

import "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/domain"

type TokenIssuer interface {
	IssueToken(user domain.User) (string, error)
}
