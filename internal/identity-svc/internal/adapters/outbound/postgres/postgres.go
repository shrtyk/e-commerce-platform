package postgres

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"

	commoncfg "github.com/shrtyk/e-commerce-platform/internal/common/config"
)

func MustCreatePostgres(cfg commoncfg.Postgres, timeouts commoncfg.Timeouts) *sql.DB {
	db, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		panic(fmt.Errorf("open postgres: %w", err))
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), timeouts.Startup)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		panic(fmt.Errorf("ping postgres: %w", err))
	}

	return db
}
