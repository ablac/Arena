package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingExecutor struct {
	query string
	args  []any
	err   error
	calls int
}

func (e *recordingExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	e.calls++
	e.query = query
	e.args = args
	return pgconn.CommandTag{}, e.err
}

func TestResetLeaderboardWith_ClearsAllLeaderboardSourcesInOneStatement(t *testing.T) {
	executor := &recordingExecutor{}

	if err := resetLeaderboardWith(context.Background(), executor); err != nil {
		t.Fatalf("resetLeaderboardWith returned an error: %v", err)
	}

	if executor.calls != 1 {
		t.Fatalf("Exec calls = %d, want 1 atomic statement", executor.calls)
	}
	if len(executor.args) != 0 {
		t.Fatalf("Exec args = %#v, want none", executor.args)
	}
	for _, table := range []string{"bot_stats", "round_bot_stats"} {
		if !strings.Contains(executor.query, table) {
			t.Errorf("reset statement %q does not clear %s", executor.query, table)
		}
	}
	if !strings.Contains(executor.query, "RESTART IDENTITY") {
		t.Errorf("reset statement %q does not reset per-round row identities", executor.query)
	}
}

func TestResetLeaderboardWith_WrapsDatabaseError(t *testing.T) {
	wantErr := errors.New("database rejected truncate")
	executor := &recordingExecutor{err: wantErr}

	err := resetLeaderboardWith(context.Background(), executor)
	if !errors.Is(err, wantErr) {
		t.Fatalf("resetLeaderboardWith error = %v, want wrapped %v", err, wantErr)
	}
}

func TestResetLeaderboard_NoPoolReturnsErrNoDatabase(t *testing.T) {
	original := Pool
	Pool = nil
	t.Cleanup(func() { Pool = original })

	if err := ResetLeaderboard(context.Background()); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("ResetLeaderboard error = %v, want %v", err, ErrNoDatabase)
	}
}
