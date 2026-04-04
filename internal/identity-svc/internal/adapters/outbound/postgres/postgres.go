package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	commonconfig "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func MustCreatePostgres(cfg commonconfig.Config) *sql.DB {
	db, err := sql.Open("pgx", cfg.Postgres.DSN())
	if err != nil {
		panic(fmt.Errorf("open postgres: %w", err))
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		panic(fmt.Errorf("ping postgres: %w", err))
	}

	return db
}
