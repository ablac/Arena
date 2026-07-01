package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectWithRetry must ride out transient DB-unavailability at startup
// (e.g. Postgres still coming up after a host/daemon restart) instead of
// giving up on the first failure as the old single-shot Connect did.

func TestConnectWithRetry_SucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	fn := func(ctx context.Context) (*pgxpool.Pool, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("db not ready yet")
		}
		return &pgxpool.Pool{}, nil
	}

	pool, err := connectWithRetry(context.Background(), 5, time.Millisecond, fn)
	if err != nil {
		t.Fatalf("expected success after transient failures, got: %v", err)
	}
	if pool == nil {
		t.Fatal("expected a non-nil pool on success")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestConnectWithRetry_FailsAfterExhaustingAttempts(t *testing.T) {
	calls := 0
	fn := func(ctx context.Context) (*pgxpool.Pool, error) {
		calls++
		return nil, errors.New("db down")
	}

	pool, err := connectWithRetry(context.Background(), 3, time.Millisecond, fn)
	if err == nil {
		t.Fatal("expected an error after exhausting all attempts")
	}
	if pool != nil {
		t.Fatal("expected a nil pool on total failure")
	}
	if calls != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", calls)
	}
}

func TestConnectWithRetry_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	fn := func(ctx context.Context) (*pgxpool.Pool, error) {
		calls++
		cancel() // simulate shutdown after the first failed attempt
		return nil, errors.New("db down")
	}

	if _, err := connectWithRetry(ctx, 10, time.Hour, fn); err == nil {
		t.Fatal("expected an error when the context is cancelled")
	}
	if calls != 1 {
		t.Fatalf("expected the retry loop to stop after cancel, got %d attempts", calls)
	}
}
