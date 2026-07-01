package security

import (
	"context"
	"testing"

	"arena-server/internal/db"
)

// TestVerifyAPIKey_NilPool_ReturnsErrorNotPanic is the end-to-end regression
// for the 2026-05-29 bot-join outage. VerifyAPIKey is called from the
// /ws/bot handler before the WebSocket upgrade; when db.Pool was nil it
// panicked inside GetAPIKeyByPrefix. It must instead return an error so the
// handler can respond with a clean auth failure.
func TestVerifyAPIKey_NilPool_ReturnsErrorNotPanic(t *testing.T) {
	orig := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = orig })

	if _, err := VerifyAPIKey(context.Background(), "arena_abcd1234efgh"); err == nil {
		t.Fatal("expected an error when db.Pool is nil, got nil")
	}
}
