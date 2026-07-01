package db

import (
	"context"
	"testing"
)

// TestAuthQueries_NilPool_ReturnErrorNotPanic is a regression test for the
// 2026-05-29 outage: when the global Pool is nil (the startup DB connect
// failed), the bot-join auth path called these query helpers, which
// dereferenced the nil pool and panicked with a nil-pointer error. chi's
// Recoverer turned every keyed /ws/bot join into a failed handshake.
//
// These helpers must return a clean error instead of panicking when the
// pool is uninitialised, so callers can reject the request gracefully.
func TestAuthQueries_NilPool_ReturnErrorNotPanic(t *testing.T) {
	orig := Pool
	Pool = nil
	t.Cleanup(func() { Pool = orig })

	ctx := context.Background()

	if _, err := GetAPIKeyByPrefix(ctx, "arena_abcd1234"); err == nil {
		t.Error("GetAPIKeyByPrefix: expected an error when Pool is nil, got nil")
	}
	if _, err := GetBotByAPIKeyID(ctx, "some-bot-id"); err == nil {
		t.Error("GetBotByAPIKeyID: expected an error when Pool is nil, got nil")
	}
	if err := UpdateAPIKeyLastSeen(ctx, "some-key-id"); err == nil {
		t.Error("UpdateAPIKeyLastSeen: expected an error when Pool is nil, got nil")
	}
	if _, err := GetAdminTokenHash(ctx, "some-token-id"); err == nil {
		t.Error("GetAdminTokenHash: expected an error when Pool is nil, got nil")
	}
}
