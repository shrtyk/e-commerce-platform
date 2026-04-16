package testhelper

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"

	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
	"github.com/shrtyk/e-commerce-platform/internal/common/tx/sqltx"
	adaptergrpc "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/grpc"
	adapterhttp "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/inbound/http"
	jwtadapter "github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/jwt"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/password/bcrypt"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/adapters/outbound/postgres/repos"
	"github.com/shrtyk/e-commerce-platform/internal/identity-svc/internal/core/service/auth"
)

const (
	TestAccessTokenKey    = "test-secret-key-for-integration-tests-32bytes"
	TestAccessTokenIssuer = "test-identity-svc"
	SessionTTL            = 24 * time.Hour

	TestPassword    = "strong-password-123"
	InvalidToken    = "not-a-valid-token"
	TestDisplayName = "Test User"
)

type TestStack struct {
	DB          *sql.DB
	AuthService *auth.AuthService
	HTTPHandler http.Handler
	GRPCServer  *grpcpkg.Server
	GRPCConn    *grpcpkg.ClientConn
}

func NewTestStack(t *testing.T, db *sql.DB) *TestStack {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	userRepository := repos.NewUserRepository(db)
	sessionRepository := repos.NewSessionRepository(db)

	txProvider := sqltx.NewProvider(db, func(tx *sql.Tx) auth.IdentityRepos {
		return auth.IdentityRepos{
			Users:    repos.NewUserRepositoryFromTx(tx),
			Sessions: repos.NewSessionRepositoryFromTx(tx),
		}
	})

	tokenIssuer := jwtadapter.NewTokenIssuer(TestAccessTokenIssuer, TestAccessTokenKey, SessionTTL)
	tokenVerifier := jwtadapter.NewTokenVerifier(TestAccessTokenKey, TestAccessTokenIssuer)
	hasher := bcrypt.NewHasher(0)

	authService := auth.NewAuthService(
		userRepository,
		sessionRepository,
		txProvider,
		hasher,
		tokenIssuer,
		SessionTTL,
	)

	tracer := noop.NewTracerProvider().Tracer("identity-svc-test")
	httpHandler := adapterhttp.NewRouter(logger, "identity-svc-test", authService, tokenVerifier, tracer)
	grpcServer := adaptergrpc.NewServer(logger, "identity-svc-test", authService, tokenVerifier, tracer)

	grpcConn, stopGRPC := commonintegration.StartBufconnGRPCServer(t, "identity-test", grpcServer)
	t.Cleanup(stopGRPC)

	return &TestStack{
		DB:          db,
		AuthService: authService,
		HTTPHandler: httpHandler,
		GRPCServer:  grpcServer,
		GRPCConn:    grpcConn,
	}
}
