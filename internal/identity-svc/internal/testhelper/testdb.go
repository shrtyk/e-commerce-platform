package testhelper

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var TestDB *sql.DB
var testContainer *tcpostgres.PostgresContainer

func StartSharedTestDB(t *testing.T) *sql.DB {
	if TestDB != nil {
		return TestDB
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(postgresDockerfile()),
		testcontainers.WithCmdArgs(
			"-c", "shared_preload_libraries=pg_cron",
			"-c", "cron.database_name=identity_test",
		),
		tcpostgres.WithDatabase("identity_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	ensureNoError(t, err, "start postgres container")

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	ensureNoError(t, err, "create postgres connection string")

	db, err := sql.Open("pgx", connectionString)
	ensureNoError(t, err, "open postgres connection")

	err = waitForDB(ctx, db)
	ensureNoError(t, err, "wait for postgres readiness")

	RunMigrations(t, db)

	TestDB = db
	testContainer = container

	return TestDB
}

func StopSharedTestDB() {
	if TestDB != nil {
		if err := TestDB.Close(); err != nil {
			panic(fmt.Errorf("close shared test database: %w", err))
		}

		TestDB = nil
	}

	if testContainer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := testContainer.Terminate(ctx); err != nil {
			panic(fmt.Errorf("terminate shared test database container: %w", err))
		}

		testContainer = nil
	}
}

func StartTestDB(t *testing.T) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	container, err := tcpostgres.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(postgresDockerfile()),
		testcontainers.WithCmdArgs(
			"-c", "shared_preload_libraries=pg_cron",
			"-c", "cron.database_name=identity_test",
		),
		tcpostgres.WithDatabase("identity_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("pgx", connectionString)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 30*time.Second, 200*time.Millisecond)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
		require.NoError(t, container.Terminate(context.Background()))
	})

	return db
}

func RunMigrations(t *testing.T, db *sql.DB) {
	if t != nil {
		t.Helper()
	}

	ensureNoError(t, goose.SetDialect("postgres"), "set goose postgres dialect")
	ensureNoError(t, goose.Up(db, migrationsDir()), "run test migrations")
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

func CleanupDB(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`TRUNCATE users, sessions RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../adapters/outbound/postgres/migrations")
}

func postgresDockerfile() testcontainers.FromDockerfile {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))

	return testcontainers.FromDockerfile{
		Context:    repoRoot,
		Dockerfile: "docker/identity/postgres/Dockerfile",
		Repo:       "identity-postgres-pgcron-test",
		Tag:        "local",
		KeepImage:  true,
	}
}
