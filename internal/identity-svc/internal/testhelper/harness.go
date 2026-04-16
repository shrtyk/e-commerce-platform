package testhelper

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"

	commonintegration "github.com/shrtyk/e-commerce-platform/internal/common/testhelper/integration"
)

type Harness struct {
	DB *sql.DB
}

var sharedPostgres commonintegration.SharedPostgres
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
		Dockerfile: postgresDockerfile(),
		CmdArgs: []string{
			"-c", "shared_preload_libraries=pg_cron",
			"-c", "cron.database_name=identity_test",
		},
		Database: "identity_test",
		Username: "postgres",
		Password: "postgres",
	})

	if err := RunMigrations(db); err != nil {
		sharedPostgres.Stop()
		commonintegration.EnsureNoError(t, fmt.Errorf("run test migrations: %w", err), "initialize shared harness")
	}

	harness := &Harness{DB: db}
	SetIntegrationHarness(harness)

	return harness
}

func StopSharedHarness() {
	SetIntegrationHarness(nil)
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

	_, err := db.Exec(`TRUNCATE users, sessions RESTART IDENTITY CASCADE`)
	commonintegration.EnsureNoError(t, err, "cleanup test database")
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../adapters/outbound/postgres/migrations")
}

func postgresDockerfile() *testcontainers.FromDockerfile {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))

	return &testcontainers.FromDockerfile{
		Context:    repoRoot,
		Dockerfile: "docker/identity/postgres/Dockerfile",
		Repo:       "identity-postgres-pgcron-test",
		Tag:        "local",
		KeepImage:  true,
	}
}

func MustDB() *sql.DB {
	harness := getIntegrationHarness()
	if harness == nil || harness.DB == nil {
		panic(fmt.Errorf("identity integration harness is not initialized"))
	}

	return harness.DB
}
