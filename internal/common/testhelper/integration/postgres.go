package integration

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type PostgresOptions struct {
	Image       string
	Dockerfile  *testcontainers.FromDockerfile
	CmdArgs     []string
	Database    string
	Username    string
	Password    string
	WaitTimeout time.Duration
}

type SharedPostgres struct {
	mu        sync.Mutex
	once      sync.Once
	initErr   error
	container *tcpostgres.PostgresContainer
	db        *sql.DB
}

func (s *SharedPostgres) Start(t *testing.T, opts PostgresOptions) *sql.DB {
	if t != nil {
		t.Helper()
	}

	s.mu.Lock()
	if s.db != nil {
		db := s.db
		s.mu.Unlock()
		return db
	}
	s.mu.Unlock()

	var initErr error
	s.once.Do(func() {
		initErr = s.initPostgres(opts)

		s.mu.Lock()
		s.initErr = initErr
		s.mu.Unlock()
	})

	s.mu.Lock()
	if s.initErr != nil {
		initErr = s.initErr
	}
	s.mu.Unlock()

	if initErr != nil {
		s.mu.Lock()
		s.once = sync.Once{}
		s.mu.Unlock()

		EnsureNoError(t, initErr, "start shared postgres")
	}

	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		EnsureNoError(t, fmt.Errorf("shared postgres is nil"), "resolve shared postgres")
	}

	return db
}

func (s *SharedPostgres) Stop() {
	s.mu.Lock()
	db := s.db
	container := s.container
	s.db = nil
	s.container = nil
	s.initErr = nil
	s.once = sync.Once{}
	s.mu.Unlock()

	if db != nil {
		if err := db.Close(); err != nil {
			panic(fmt.Errorf("close shared postgres: %w", err))
		}
	}

	if container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := container.Terminate(ctx); err != nil {
			panic(fmt.Errorf("terminate shared postgres container: %w", err))
		}
	}
}

func (s *SharedPostgres) initPostgres(opts PostgresOptions) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if opts.WaitTimeout <= 0 {
		opts.WaitTimeout = 30 * time.Second
	}

	runOpts := []testcontainers.ContainerCustomizer{
		tcpostgres.WithDatabase(opts.Database),
		tcpostgres.WithUsername(opts.Username),
		tcpostgres.WithPassword(opts.Password),
		tcpostgres.BasicWaitStrategies(),
	}

	if opts.Dockerfile != nil {
		runOpts = append(runOpts, testcontainers.WithDockerfile(*opts.Dockerfile))
	}

	if len(opts.CmdArgs) > 0 {
		runOpts = append(runOpts, testcontainers.WithCmdArgs(opts.CmdArgs...))
	}

	container, err := tcpostgres.Run(ctx, opts.Image, runOpts...)
	if err != nil {
		return fmt.Errorf("start postgres container: %w", err)
	}

	defer func() {
		if err == nil {
			return
		}

		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = container.Terminate(cleanupCtx)
	}()

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("build postgres connection string: %w", err)
	}

	db, err := sql.Open("pgx", connectionString)
	if err != nil {
		return fmt.Errorf("open postgres connection: %w", err)
	}

	defer func() {
		if err == nil {
			return
		}

		_ = db.Close()
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), opts.WaitTimeout)
	defer waitCancel()

	if err := waitForDB(waitCtx, db); err != nil {
		return fmt.Errorf("wait for postgres readiness: %w", err)
	}

	s.mu.Lock()
	s.db = db
	s.container = container
	s.mu.Unlock()

	return nil
}

func waitForDB(ctx context.Context, db *sql.DB) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
