package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresIntegrationEnv = "ARENA_TEST_DATABASE_URL"

func useFreshPostgresSchema(t *testing.T) context.Context {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv(postgresIntegrationEnv))
	if databaseURL == "" {
		t.Skipf("set %s to run PostgreSQL integration tests", postgresIntegrationEnv)
	}

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer setupCancel()

	adminPool, err := pgxpool.New(setupCtx, databaseURL)
	if err != nil {
		t.Fatalf("create integration admin pool: %v", err)
	}
	if err := adminPool.Ping(setupCtx); err != nil {
		adminPool.Close()
		t.Fatalf("ping integration Postgres: %v", err)
	}

	schema := "arena_integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(setupCtx, "CREATE SCHEMA "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatalf("create integration schema: %v", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		adminPool.Close()
		t.Fatalf("parse integration database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	testPool, err := pgxpool.NewWithConfig(setupCtx, poolConfig)
	if err != nil {
		adminPool.Close()
		t.Fatalf("create schema-scoped pool: %v", err)
	}
	if err := testPool.Ping(setupCtx); err != nil {
		testPool.Close()
		adminPool.Close()
		t.Fatalf("ping schema-scoped pool: %v", err)
	}

	previousPool := Pool
	Pool = testPool
	t.Cleanup(func() {
		Pool = previousPool
		testPool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop integration schema: %v", err)
		}
		adminPool.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPostgresFreshSchemaCosmeticsAndLeaderboardResetSmoke(t *testing.T) {
	ctx := useFreshPostgresSchema(t)

	// Startup runs schema repair against both empty and existing databases.
	// Running it twice catches non-idempotent DDL and seed behavior.
	for attempt := 1; attempt <= 2; attempt++ {
		if err := EnsureCoreSchema(ctx); err != nil {
			t.Fatalf("EnsureCoreSchema attempt %d: %v", attempt, err)
		}
	}

	const registrationLimit = 5
	type rateResult struct {
		allowed bool
		err     error
	}
	rateResults := make(chan rateResult, registrationLimit*4)
	for i := 0; i < cap(rateResults); i++ {
		go func() {
			allowed, _, err := CheckRateLimit(context.Background(), "198.51.100.25", registrationLimit)
			rateResults <- rateResult{allowed: allowed, err: err}
		}()
	}
	allowedRegistrations := 0
	for i := 0; i < cap(rateResults); i++ {
		result := <-rateResults
		if result.err != nil {
			t.Fatalf("concurrent CheckRateLimit: %v", result.err)
		}
		if result.allowed {
			allowedRegistrations++
		}
	}
	if allowedRegistrations != registrationLimit {
		t.Fatalf("atomic registration admissions = %d, want %d", allowedRegistrations, registrationLimit)
	}
	var consumedSlots int
	if err := Pool.QueryRow(ctx, `
		SELECT keys_generated FROM rate_limits WHERE ip_address = $1`, "198.51.100.25").Scan(&consumedSlots); err != nil {
		t.Fatalf("read registration rate limit: %v", err)
	}
	if consumedSlots != registrationLimit {
		t.Fatalf("registration slots stored = %d, want %d", consumedSlots, registrationLimit)
	}

	now := time.Now()
	bot := &Bot{
		ID:              "integration-bot",
		APIKeyID:        "integration-key",
		Name:            "Integration Bot",
		AvatarColor:     "#123456",
		DefaultWeapon:   "sword",
		DefaultStats:    JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := CreateAPIKeyAndBot(ctx, bot.APIKeyID, "integration-hash", "arena_integration_prefix", "127.0.0.1", bot); err != nil {
		t.Fatalf("CreateAPIKeyAndBot: %v", err)
	}

	created, err := GrantCosmeticEntitlement(ctx, bot.ID, "skin-neon-grid", "integration", "grant-1")
	if err != nil || !created {
		t.Fatalf("GrantCosmeticEntitlement = (%v, %v), want (true, nil)", created, err)
	}
	item, err := EquipCosmetic(ctx, bot.ID, CosmeticSlotBotSkin, "skin-neon-grid")
	if err != nil {
		t.Fatalf("EquipCosmetic: %v", err)
	}
	if item.ID != "skin-neon-grid" || item.AssetKey != "neon_grid" {
		t.Fatalf("equipped item = %+v", item)
	}
	equipped, err := GetEquippedCosmetics(ctx, bot.ID)
	if err != nil || equipped[CosmeticSlotBotSkin] != "neon_grid" {
		t.Fatalf("equipped cosmetics = (%v, %v), want neon_grid", equipped, err)
	}

	revoked, err := RevokeCosmeticEntitlement(ctx, bot.ID, "skin-neon-grid")
	if err != nil || !revoked {
		t.Fatalf("RevokeCosmeticEntitlement = (%v, %v), want (true, nil)", revoked, err)
	}
	equipped, err = GetEquippedCosmetics(ctx, bot.ID)
	if err != nil || equipped[CosmeticSlotBotSkin] != "standard" {
		t.Fatalf("post-revoke cosmetics = (%v, %v), want standard", equipped, err)
	}

	if err := ApplyBotStatsDelta(ctx, &BotStatsDelta{
		BotID: bot.ID, Kills: 3, Deaths: 1, DamageDealt: 90, DamageTaken: 30,
		CurrentStreak: 2, BestStreak: 2, Elo: 1040, RoundsPlayed: 1,
		RoundWins: 1, CapturedAt: now,
	}); err != nil {
		t.Fatalf("ApplyBotStatsDelta: %v", err)
	}
	if err := InsertRoundBotStats(ctx, 1, bot.ID, bot.Name, "sword", 3, 1, 90, 30, 60, 8, 5, 1, 20, 1040, true); err != nil {
		t.Fatalf("InsertRoundBotStats: %v", err)
	}
	if err := ResetLeaderboard(ctx); err != nil {
		t.Fatalf("ResetLeaderboard: %v", err)
	}

	var allTimeRows, windowRows int
	if err := Pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM bot_stats),
		       (SELECT COUNT(*) FROM round_bot_stats)`).Scan(&allTimeRows, &windowRows); err != nil {
		t.Fatalf("count reset leaderboard rows: %v", err)
	}
	if allTimeRows != 0 || windowRows != 0 {
		t.Fatalf("leaderboard reset left rows: all_time=%d windows=%d", allTimeRows, windowRows)
	}
}

// TestPostgresCosmeticEntitlementSerialization covers races that a mocked
// store cannot reproduce: revoke must take the entitlement lock before it
// deletes the loadout, and an idempotent grant must serialize with a concurrent
// entitlement delete before acknowledging fulfillment.
func TestPostgresCosmeticEntitlementSerialization(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	now := time.Now()
	bot := &Bot{
		ID: "race-bot", APIKeyID: "race-key", Name: "Race Bot", AvatarColor: "#abcdef",
		DefaultWeapon: "sword", DefaultStats: JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive", CreatedAt: now, UpdatedAt: now,
	}
	if err := CreateAPIKeyAndBot(ctx, bot.APIKeyID, "race-hash", "arena_race_prefix", "127.0.0.1", bot); err != nil {
		t.Fatalf("CreateAPIKeyAndBot: %v", err)
	}
	if created, err := GrantCosmeticEntitlement(ctx, bot.ID, "skin-neon-grid", "integration", "race-grant"); err != nil || !created {
		t.Fatalf("grant race entitlement = (%v, %v)", created, err)
	}

	locker, err := Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin entitlement locker: %v", err)
	}
	defer locker.Rollback(context.Background())
	var marker int
	if err := locker.QueryRow(ctx, `
		SELECT 1 FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2
		FOR UPDATE`, bot.ID, "skin-neon-grid").Scan(&marker); err != nil {
		t.Fatalf("lock entitlement: %v", err)
	}

	type revokeResult struct {
		revoked bool
		err     error
	}
	resultCh := make(chan revokeResult, 1)
	go func() {
		revoked, err := RevokeCosmeticEntitlement(context.Background(), bot.ID, "skin-neon-grid")
		resultCh <- revokeResult{revoked: revoked, err: err}
	}()

	waitForBlockedCosmeticStatement(t, ctx)
	if _, err := locker.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id)
		VALUES ($1, $2, $3)`, bot.ID, CosmeticSlotBotSkin, "skin-neon-grid"); err != nil {
		t.Fatalf("insert concurrent paid loadout: %v", err)
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent equip: %v", err)
	}

	select {
	case result := <-resultCh:
		if result.err != nil || !result.revoked {
			t.Fatalf("concurrent revoke = (%v, %v)", result.revoked, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent revoke did not finish")
	}

	var loadoutRows, entitlementRows int
	if err := Pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM bot_cosmetic_loadout WHERE bot_id = $1),
		       (SELECT COUNT(*) FROM cosmetic_entitlements WHERE bot_id = $1)`, bot.ID).
		Scan(&loadoutRows, &entitlementRows); err != nil {
		t.Fatalf("count post-race cosmetic rows: %v", err)
	}
	if loadoutRows != 0 || entitlementRows != 0 {
		t.Fatalf("revoke left paid state: loadout=%d entitlements=%d", loadoutRows, entitlementRows)
	}

	if created, err := GrantCosmeticEntitlement(ctx, bot.ID, "skin-neon-grid", "integration", "race-regrant"); err != nil || !created {
		t.Fatalf("create grant/revoke race entitlement = (%v, %v)", created, err)
	}
	deleteTx, err := Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin concurrent entitlement delete: %v", err)
	}
	defer deleteTx.Rollback(context.Background())
	if _, err := deleteTx.Exec(ctx, `
		DELETE FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2`, bot.ID, "skin-neon-grid"); err != nil {
		t.Fatalf("stage concurrent entitlement delete: %v", err)
	}

	type grantResult struct {
		created bool
		err     error
	}
	grantCh := make(chan grantResult, 1)
	go func() {
		created, err := GrantCosmeticEntitlement(context.Background(), bot.ID, "skin-neon-grid", "integration", "race-regrant")
		grantCh <- grantResult{created: created, err: err}
	}()

	waitForBlockedCosmeticStatement(t, ctx)
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent entitlement delete: %v", err)
	}
	select {
	case result := <-grantCh:
		if result.err != nil || !result.created {
			t.Fatalf("grant racing revoke = (%v, %v), want newly fulfilled entitlement", result.created, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("grant racing revoke did not finish")
	}

	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2`, bot.ID, "skin-neon-grid").Scan(&entitlementRows); err != nil {
		t.Fatalf("count regranted entitlement: %v", err)
	}
	if entitlementRows != 1 {
		t.Fatalf("acknowledged regrant left %d entitlements, want 1", entitlementRows)
	}
	var legacyStatus string
	if err := Pool.QueryRow(ctx, `
		SELECT status FROM cosmetic_licenses
		WHERE legacy_bot_id = $1 AND cosmetic_id = $2`, bot.ID, "skin-neon-grid").Scan(&legacyStatus); err != nil {
		t.Fatalf("load terminal legacy license after regrant: %v", err)
	}
	if legacyStatus != "revoked" {
		t.Fatalf("legacy regrant resurrected terminal license status = %q, want revoked", legacyStatus)
	}
}

func waitForBlockedCosmeticStatement(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		err := Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%cosmetic_entitlements%'
			)`).Scan(&waiting)
		if err != nil {
			t.Fatalf("inspect blocked cosmetic statement: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for blocked cosmetic statement")
}
