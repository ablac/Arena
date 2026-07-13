package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingAPIKeyAuthExecer struct {
	calls int
	query string
	args  []any
	err   error
}

func (e *recordingAPIKeyAuthExecer) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	e.calls++
	e.query = query
	e.args = args
	return pgconn.CommandTag{}, e.err
}

func TestUpdateAPIKeyHashAndLastSeenUsesOneWrite(t *testing.T) {
	execer := &recordingAPIKeyAuthExecer{}
	err := updateAPIKeyHashAndLastSeen(context.Background(), execer, "key-1", "rollback-safe-composite")
	if err != nil {
		t.Fatalf("updateAPIKeyHashAndLastSeen: %v", err)
	}
	if execer.calls != 1 {
		t.Fatalf("Exec calls = %d, want 1", execer.calls)
	}
	if !strings.Contains(execer.query, "key_hash = $2") || !strings.Contains(execer.query, "last_seen = NOW()") {
		t.Fatalf("update query does not atomically replace hash and last_seen: %q", execer.query)
	}
	if len(execer.args) != 2 || execer.args[0] != "key-1" || execer.args[1] != "rollback-safe-composite" {
		t.Fatalf("Exec args = %#v", execer.args)
	}
}

func TestUpdateAPIKeyHashAndLastSeenRejectsEmptyHash(t *testing.T) {
	execer := &recordingAPIKeyAuthExecer{}
	if err := updateAPIKeyHashAndLastSeen(context.Background(), execer, "key-1", ""); err == nil {
		t.Fatal("empty replacement hash was accepted")
	}
	if execer.calls != 0 {
		t.Fatalf("Exec calls = %d, want 0", execer.calls)
	}
}

func TestUpdateAPIKeyHashAndLastSeenWrapsDatabaseError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	execer := &recordingAPIKeyAuthExecer{err: wantErr}
	if err := updateAPIKeyHashAndLastSeen(context.Background(), execer, "key-1", "rollback-safe-composite"); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestUpdateAPIKeyHashAndLastSeenWithoutPoolReturnsError(t *testing.T) {
	originalPool := Pool
	Pool = nil
	t.Cleanup(func() { Pool = originalPool })

	if err := UpdateAPIKeyHashAndLastSeen(context.Background(), "key-1", "rollback-safe-composite"); !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("error = %v, want %v", err, ErrNoDatabase)
	}
}
