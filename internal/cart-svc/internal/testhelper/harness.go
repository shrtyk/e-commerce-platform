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

	"github.com/pressly/goose/v3"
	redislib "github.com/redis/go-redis/v9"

	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
)

type Harness struct {
	DB          *sql.DB
	RedisClient *redislib.Client
}

var sharedPostgres commonintegration.SharedPostgres
var sharedRedis commonintegration.SharedRedis
var (
	integrationHarnessMu sync.RWMutex
	integrationHarness   *Harness
)

func SetIntegrationHarness(harness *Harness) {
	integrationHarnessMu.Lock()
	integrationHarness = harness
	integrationHarnessMu.Unlock()
}

func getIntegrationHarness() *Harness {
	integrationHarnessMu.RLock()
	harness := integrationHarness
	integrationHarnessMu.RUnlock()

	return harness
}

func IntegrationHarness(t *testing.T) *Harness {
	t.Helper()
	harness := getIntegrationHarness()
	if harness == nil {
		t.Fatal("integration harness is not initialized")
	}

	return harness
}

func StartSharedHarness(t *testing.T) *Harness {
	if t != nil {
		t.Helper()
	}

	db := sharedPostgres.Start(t, commonintegration.PostgresOptions{
		Image:    "postgres:18",
		Database: "cart_test",
		Username: "postgres",
		Password: "postgres",
	})
	redisClient := sharedRedis.Start(t, commonintegration.RedisOptions{Image: "redis:8-alpine"})

	if err := RunMigrations(db); err != nil {
		sharedRedis.Stop()
		sharedPostgres.Stop()
		commonintegration.EnsureNoError(t, fmt.Errorf("run test migrations: %w", err), "initialize shared harness")
	}

	harness := &Harness{
		DB:          db,
		RedisClient: redisClient,
	}
	SetIntegrationHarness(harness)

	return harness
}

func StopSharedHarness() {
	SetIntegrationHarness(nil)
	sharedRedis.Stop()
	sharedPostgres.Stop()
}

func RunMigrations(db *sql.DB) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose postgres dialect: %w", err)
	}

	if err := goose.Up(db, migrationsDir()); err != nil {
		return fmt.Errorf("run test migrations: %w", err)
	}

	return nil
}

func CleanupDB(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.ExecContext(ctx, `TRUNCATE cart_items, carts, product_snapshots RESTART IDENTITY CASCADE`)
	if err != nil {
		err = fmt.Errorf("cleanup db truncate within timeout: %w", err)
	}
	commonintegration.EnsureNoError(t, err, "cleanup test database")
}

func CleanupRedis(t *testing.T, client *redislib.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.FlushDB(ctx).Err()
	if err != nil {
		err = fmt.Errorf("flush redis within timeout: %w", err)
	}
	commonintegration.EnsureNoError(t, err, "flush redis")
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../adapters/outbound/postgres/migrations")
}
