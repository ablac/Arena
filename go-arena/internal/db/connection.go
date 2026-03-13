package db

import (
	"context"
	"fmt"
	"log/slog"

	"arena-server/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the global database connection pool.
var Pool *pgxpool.Pool

// Connect creates a new pgx connection pool using values from config.C.
func Connect(ctx context.Context) error {
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		config.C.DBUser,
		config.C.DBPassword,
		config.C.DBHost,
		config.C.DBPort,
		config.C.DBName,
	)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}
	poolCfg.MaxConns = int32(config.C.DBPoolSize + config.C.DBMaxOverflow)
	poolCfg.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	Pool = pool
	slog.Info("database connected",
		"host", config.C.DBHost,
		"port", config.C.DBPort,
		"db", config.C.DBName,
	)
	return nil
}

// Close closes the database connection pool.
func Close() {
	if Pool != nil {
		Pool.Close()
		slog.Info("database connection closed")
	}
}
