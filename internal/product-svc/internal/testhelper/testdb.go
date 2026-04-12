package testhelper

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var TestDB *sql.DB
var sharedContainer *tcpostgres.PostgresContainer
var sharedMu sync.Mutex
var sharedOnce sync.Once
var sharedInitErr error

func StartSharedTestDB(t *testing.T) *sql.DB {
	if t != nil {
		t.Helper()
	}

	sharedMu.Lock()
	if TestDB != nil {
		db := TestDB
		sharedMu.Unlock()
		return db
	}
	sharedMu.Unlock()

	var initErr error
	sharedOnce.Do(func() {
		initErr = initSharedTestDB()
		sharedMu.Lock()
		sharedInitErr = initErr
		sharedMu.Unlock()
	})

	sharedMu.Lock()
	if sharedInitErr != nil {
		initErr = sharedInitErr
	}
	sharedMu.Unlock()

	if initErr != nil {
		sharedMu.Lock()
		sharedOnce = sync.Once{}
		sharedMu.Unlock()

		ensureNoError(t, initErr, "start shared test database")
	}

	sharedMu.Lock()
	db := TestDB
	sharedMu.Unlock()
	if db == nil {
		ensureNoError(t, fmt.Errorf("shared test database is nil"), "resolve shared test database")
	}

	return db
}

func initSharedTestDB() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:18",
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp")),
		tcpostgres.WithDatabase("product_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
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

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer pingCancel()
	for {
		if pingErr := db.PingContext(pingCtx); pingErr == nil {
			break
		}

		select {
		case <-pingCtx.Done():
			return fmt.Errorf("ping postgres: %w", pingCtx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.Up(db, migrationsDir()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	sharedMu.Lock()
	TestDB = db
	sharedContainer = container
	sharedMu.Unlock()

	return nil
}

func StopSharedTestDB() {
	sharedMu.Lock()
	db := TestDB
	container := sharedContainer
	TestDB = nil
	sharedContainer = nil
	sharedInitErr = nil
	sharedOnce = sync.Once{}
	sharedMu.Unlock()

	if db != nil {
		if err := db.Close(); err != nil {
			panic(fmt.Errorf("close shared test database: %w", err))
		}
	}

	if container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := container.Terminate(ctx); err != nil {
			panic(fmt.Errorf("terminate shared test database container: %w", err))
		}
	}
}

func RunMigrations(t *testing.T, db *sql.DB) {
	if t != nil {
		t.Helper()
	}

	ensureNoError(t, goose.SetDialect("postgres"), "set goose postgres dialect")
	ensureNoError(t, goose.Up(db, migrationsDir()), "run test migrations")
}

func CleanupDB(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`TRUNCATE stock_records, products RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../adapters/outbound/postgres/migrations")
}

func ensureNoError(t *testing.T, err error, action string) {
	if err == nil {
		return
	}

	if t != nil {
		require.NoError(t, err)
		return
	}

	panic(fmt.Errorf("%s: %w", action, err))
}
