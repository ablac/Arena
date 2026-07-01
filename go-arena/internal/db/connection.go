package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"arena-server/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the global database connection pool.
var Pool *pgxpool.Pool

// ErrNoDatabase is returned by query helpers when the connection pool has
// not been initialised (db.Connect failed or was never called). Returning
// this instead of dereferencing a nil pool prevents the nil-pointer panic
// that took the keyed bot-join path down in the 2026-05-29 incident.
var ErrNoDatabase = errors.New("database connection pool is not initialized")

// Connect establishes the global pgx pool from config.C. It retries on
// failure (config.C.DBConnectAttempts, spaced by DBConnectRetrySeconds) so a
// database that is still coming up after a host or Docker daemon restart does
// not permanently leave the server without persistence -- the failure mode
// behind the 2026-05-29 bot-join outage.
func Connect(ctx context.Context) error {
	delay := time.Duration(config.C.DBConnectRetrySeconds) * time.Second

	pool, err := connectWithRetry(ctx, config.C.DBConnectAttempts, delay, connectOnce)
	if err != nil {
		return err
	}

	Pool = pool
	slog.Info("database connected",
		"host", config.C.DBHost,
		"port", config.C.DBPort,
		"db", config.C.DBName,
	)
	return nil
}

// connectOnce performs a single connect-and-ping attempt and returns the
// resulting pool. It is the unit of work retried by connectWithRetry.
func connectOnce(ctx context.Context) (*pgxpool.Pool, error) {
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
		return nil, fmt.Errorf("failed to parse connection config: %w", err)
	}
	poolCfg.MaxConns = int32(config.C.DBPoolSize + config.C.DBMaxOverflow)
	poolCfg.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// connectWithRetry calls connectFn up to attempts times, waiting delay between
// attempts, until it succeeds or the context is cancelled. It is factored out
// from Connect so the retry policy can be unit-tested without a real database.
func connectWithRetry(
	ctx context.Context,
	attempts int,
	delay time.Duration,
	connectFn func(context.Context) (*pgxpool.Pool, error),
) (*pgxpool.Pool, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		pool, err := connectFn(ctx)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		slog.Warn("database connect attempt failed",
			"attempt", i+1, "of", attempts, "error", err)
	}
	return nil, lastErr
}

// Close closes the database connection pool.
func Close() {
	if Pool != nil {
		Pool.Close()
		slog.Info("database connection closed")
	}
}
