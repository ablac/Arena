package db

import (
	"context"
	"reflect"
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
	if _, err := GetAllAdminTokenHashes(ctx); err == nil {
		t.Error("GetAllAdminTokenHashes: expected an error when Pool is nil, got nil")
	}
}

func TestMergeCanonicalWeaponKillStats(t *testing.T) {
	raw := []WeaponKillStats{
		{Weapon: "staff", Kills: 3, Kills24h: 2, Kills1h: 1, FinisherDamage: 30},
		{Weapon: "staff_burn", Kills: 4, Kills24h: 3, Kills1h: 2, FinisherDamage: 40},
		{Weapon: "grapple_slam", Kills: 2, Kills24h: 1, FinisherDamage: 20},
		{Weapon: "bow", Kills: 1, Kills24h: 1, Kills1h: 1, FinisherDamage: 10},
	}
	want := []WeaponKillStats{
		{Weapon: "bow", Kills: 1, Kills24h: 1, Kills1h: 1, FinisherDamage: 10},
		{Weapon: "grapple", Kills: 2, Kills24h: 1, FinisherDamage: 20},
		{Weapon: "staff", Kills: 7, Kills24h: 5, Kills1h: 3, FinisherDamage: 70},
	}

	if got := mergeCanonicalWeaponKillStats(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical kill stats = %#v, want %#v", got, want)
	}
}
