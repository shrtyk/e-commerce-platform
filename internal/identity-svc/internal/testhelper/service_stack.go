package testhelper

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
	grpcpkg "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

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
	bufconnBufferSize     = 1024 * 1024

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

	listener := bufconn.Listen(bufconnBufferSize)
	ready := make(chan struct{})
	serveDone := make(chan struct{})
	readyListener := &readySignalListener{
		Listener: listener,
		ready:    ready,
	}

	go func() {
		defer close(serveDone)
		_ = grpcServer.Serve(readyListener)
	}()

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
		t.Fatal("grpc server did not start accepting connections")
	}

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.Dial()
	}

	grpcConn, err := grpcpkg.NewClient(
		"passthrough:///identity-test",
		grpcpkg.WithContextDialer(dialer),
		grpcpkg.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
		t.Fatalf("create grpc client connection: %v", err)
	}

	t.Cleanup(func() {
		require.NoError(t, grpcConn.Close())
		grpcServer.Stop()
		require.NoError(t, listener.Close())
		<-serveDone
	})

	return &TestStack{
		DB:          db,
		AuthService: authService,
		HTTPHandler: httpHandler,
		GRPCServer:  grpcServer,
		GRPCConn:    grpcConn,
	}
}

type readySignalListener struct {
	net.Listener
	once  sync.Once
	ready chan struct{}
}

func (l *readySignalListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		close(l.ready)
	})

	return l.Listener.Accept()
}
