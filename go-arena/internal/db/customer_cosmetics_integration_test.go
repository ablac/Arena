package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func createCustomerCosmeticsTestBot(t *testing.T, ctx context.Context, suffix string) *Bot {
	t.Helper()
	now := time.Now().UTC()
	bot := &Bot{
		ID:              "customer-bot-" + suffix,
		APIKeyID:        "customer-key-" + suffix,
		Name:            "Customer Bot " + suffix,
		AvatarColor:     "#123456",
		DefaultWeapon:   "sword",
		DefaultStats:    JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := CreateAPIKeyAndBot(ctx, bot.APIKeyID, "hash-"+suffix, "arena_customer_"+suffix, "127.0.0.1", bot); err != nil {
		t.Fatalf("CreateAPIKeyAndBot(%s): %v", suffix, err)
	}
	return bot
}

func TestPostgresCustomerCosmeticsAccountOwnershipAndExclusiveAssignment(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	botOne := createCustomerCosmeticsTestBot(t, ctx, "one")
	botTwo := createCustomerCosmeticsTestBot(t, ctx, "two")
	botOther := createCustomerCosmeticsTestBot(t, ctx, "other")

	// Fulfillment may arrive before first login. The verified identity claims
	// exactly that pending email account; a different identity cannot reuse it.
	firstLicense, created, err := GrantCosmeticLicense(ctx, "Owner@Example.com", "skin-neon-grid", "stripe", "checkout-1")
	if err != nil || !created {
		t.Fatalf("GrantCosmeticLicense first = (%+v, %v, %v)", firstLicense, created, err)
	}
	pendingAccountID := *firstLicense.AccountID
	account, err := UpsertVerifiedCustomerAccount(ctx, "owner@example.com", "https://id.example", "owner-subject", "Owner")
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	if account.ID != pendingAccountID || account.EmailVerifiedAt == nil {
		t.Fatalf("verified account = %+v, pending account ID = %s", account, pendingAccountID)
	}
	if _, err := UpsertVerifiedCustomerAccount(ctx, "owner@example.com", "https://id.example", "attacker-subject", "Other"); !errors.Is(err, ErrCustomerIdentityConflict) {
		t.Fatalf("second identity bind error = %v, want ErrCustomerIdentityConflict", err)
	}

	idempotent, created, err := GrantCosmeticLicense(ctx, "owner@example.com", "skin-neon-grid", "stripe", "checkout-1")
	if err != nil || created || idempotent.ID != firstLicense.ID {
		t.Fatalf("idempotent grant = (%+v, %v, %v)", idempotent, created, err)
	}
	if _, _, err := GrantCosmeticLicense(ctx, "owner@example.com", "weapon-solar-flare", "stripe", "checkout-1"); !errors.Is(err, ErrCosmeticLicenseGrantConflict) {
		t.Fatalf("conflicting external reference error = %v", err)
	}
	if _, _, err := GrantCosmeticLicense(ctx, "owner@example.com", "weapon-solar-flare", "stripe", ""); !errors.Is(err, ErrCosmeticLicenseReferenceRequired) {
		t.Fatalf("provider grant without reference error = %v, want ErrCosmeticLicenseReferenceRequired", err)
	}

	linkedBotOne, err := LinkBotToCustomerAccount(ctx, account.ID, botOne.ID)
	if err != nil {
		t.Fatalf("link bot one: %v", err)
	}
	if linkedBotOne.AvatarColor != botOne.AvatarColor || linkedBotOne.DefaultWeapon != botOne.DefaultWeapon {
		t.Fatalf("linked bot one preview metadata = %+v, want color=%q weapon=%q", linkedBotOne, botOne.AvatarColor, botOne.DefaultWeapon)
	}
	if _, err := LinkBotToCustomerAccount(ctx, account.ID, botTwo.ID); err != nil {
		t.Fatalf("link bot two: %v", err)
	}
	linkedBots, err := ListAccountBots(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListAccountBots: %v", err)
	}
	if len(linkedBots) != 2 || linkedBots[0].AvatarColor != botOne.AvatarColor || linkedBots[0].DefaultWeapon != botOne.DefaultWeapon {
		t.Fatalf("linked bot inventory preview metadata = %+v", linkedBots)
	}
	change, err := AssignCosmeticLicense(ctx, account.ID, firstLicense.ID, &botOne.ID)
	if err != nil || change.CurrentBotID == nil || *change.CurrentBotID != botOne.ID {
		t.Fatalf("assign first license = (%+v, %v)", change, err)
	}
	if _, err := EquipCustomerCosmeticLicense(ctx, account.ID, botOne.ID, firstLicense.ID); err != nil {
		t.Fatalf("equip exact first license: %v", err)
	}
	if _, err := AssignCosmeticLicense(ctx, account.ID, firstLicense.ID, &botOne.ID); err != nil {
		t.Fatalf("repeat same assignment: %v", err)
	}
	equippedAfterIdempotentAssign, err := GetEquippedCosmetics(ctx, botOne.ID)
	if err != nil || equippedAfterIdempotentAssign[CosmeticSlotBotSkin] != "neon_grid" {
		t.Fatalf("same assignment removed exact loadout: (%v, %v)", equippedAfterIdempotentAssign, err)
	}

	// Each purchase creates a stable, independently assignable copy.
	secondLicense, created, err := GrantCosmeticLicense(ctx, "owner@example.com", "skin-neon-grid", "manual", "")
	if err != nil || !created || secondLicense.ID == firstLicense.ID {
		t.Fatalf("second copy = (%+v, %v, %v)", secondLicense, created, err)
	}
	if _, err := AssignCosmeticLicense(ctx, account.ID, secondLicense.ID, &botTwo.ID); err != nil {
		t.Fatalf("assign second copy: %v", err)
	}
	if _, err := EquipCustomerCosmeticLicense(ctx, account.ID, botOne.ID, secondLicense.ID); !errors.Is(err, ErrCustomerBotNotLinked) {
		t.Fatalf("equip license on wrong bot error = %v", err)
	}

	// PostgreSQL itself enforces one assignment per license.
	_, err = Pool.Exec(ctx, `
		INSERT INTO cosmetic_license_assignments (license_id, account_id, bot_id)
		VALUES ($1, $2, $3)`, firstLicense.ID, account.ID, botTwo.ID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate license assignment error = %v, want unique violation", err)
	}

	otherAccount, err := UpsertVerifiedCustomerAccount(ctx, "other@example.com", "https://id.example", "other-subject", "Other")
	if err != nil {
		t.Fatalf("create other account: %v", err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, otherAccount.ID, botOther.ID); err != nil {
		t.Fatalf("link other bot: %v", err)
	}
	thirdLicense, _, err := GrantCosmeticLicense(ctx, "owner@example.com", "weapon-solar-flare", "manual", "copy-3")
	if err != nil {
		t.Fatalf("grant third license: %v", err)
	}
	_, err = Pool.Exec(ctx, `
		INSERT INTO cosmetic_license_assignments (license_id, account_id, bot_id)
		VALUES ($1, $2, $3)`, thirdLicense.ID, otherAccount.ID, botOther.ID)
	pgErr = nil
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("cross-account assignment error = %v, want FK violation", err)
	}
	if _, err := Pool.Exec(ctx, `UPDATE cosmetic_licenses SET status = 'refunded' WHERE id = $1`, thirdLicense.ID); err != nil {
		t.Fatalf("mark exact copy refunded: %v", err)
	}
	if _, revoked, err := RevokeCosmeticLicense(ctx, thirdLicense.ID); err != nil || revoked {
		t.Fatalf("revoke refunded copy = (%v, %v), want unchanged", revoked, err)
	}
	refunded, err := getCosmeticLicense(ctx, thirdLicense.ID)
	if err != nil || refunded.Status != "refunded" {
		t.Fatalf("terminal status after revoke = (%+v, %v), want refunded", refunded, err)
	}

	// A revoked key keeps ownership intact but cannot receive a new assignment
	// or equip an already-assigned license. Unassign remains available.
	if err := DeactivateAPIKey(ctx, botTwo.APIKeyID); err != nil {
		t.Fatalf("DeactivateAPIKey: %v", err)
	}
	if _, err := AssignCosmeticLicense(ctx, account.ID, firstLicense.ID, &botTwo.ID); !errors.Is(err, ErrCustomerBotKeyInactive) {
		t.Fatalf("assignment to inactive bot error = %v", err)
	}
	if _, err := EquipCustomerCosmeticLicense(ctx, account.ID, botTwo.ID, secondLicense.ID); !errors.Is(err, ErrCustomerBotKeyInactive) {
		t.Fatalf("equip on inactive bot error = %v", err)
	}
	if _, err := AssignCosmeticLicense(ctx, account.ID, secondLicense.ID, nil); err != nil {
		t.Fatalf("unassign from inactive bot: %v", err)
	}

	assignment, revoked, err := RevokeCosmeticLicense(ctx, firstLicense.ID)
	if err != nil || !revoked || assignment.PreviousBotID == nil || *assignment.PreviousBotID != botOne.ID {
		t.Fatalf("revoke first license = (%+v, %v, %v)", assignment, revoked, err)
	}
	if _, revokedAgain, err := RevokeCosmeticLicense(ctx, firstLicense.ID); err != nil || revokedAgain {
		t.Fatalf("idempotent revoke = (%v, %v)", revokedAgain, err)
	}
	equipped, err := GetEquippedCosmetics(ctx, botOne.ID)
	if err != nil || equipped[CosmeticSlotBotSkin] != "standard" {
		t.Fatalf("post-revoke equipped cosmetics = (%v, %v)", equipped, err)
	}

	for index, preservedStatus := range []string{"refunded", "chargeback"} {
		license, _, err := GrantCosmeticLicense(ctx, account.Email, "attachment-orbital-halo", "manual",
			fmt.Sprintf("preserve-status-%d", index))
		if err != nil {
			t.Fatalf("grant %s license: %v", preservedStatus, err)
		}
		if _, err := Pool.Exec(ctx, `UPDATE cosmetic_licenses SET status = $2 WHERE id = $1`, license.ID, preservedStatus); err != nil {
			t.Fatalf("set %s status: %v", preservedStatus, err)
		}
		change, revoked, err := RevokeCosmeticLicense(ctx, license.ID)
		if err != nil || revoked || change.License.Status != preservedStatus {
			t.Fatalf("revoke preserved status %s = (%+v, %v, %v)", preservedStatus, change, revoked, err)
		}
	}
}

func TestPostgresExactPR69CosmeticsSchemaUpgradeAndLegacyRevoke(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	// Recreate the exact cosmetics-related shape that PR #69 shipped: no
	// customer/account tables and no license/account columns on the loadout.
	_, err := Pool.Exec(ctx, `
		CREATE TABLE api_keys (id TEXT PRIMARY KEY);
		CREATE TABLE bots (
			id TEXT PRIMARY KEY,
			api_key_id TEXT NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT 'Unnamed Bot'
		);
		CREATE TABLE cosmetic_items (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			slot TEXT NOT NULL CHECK (slot IN ('bot_skin', 'weapon_skin', 'attachment')),
			asset_key TEXT NOT NULL,
			rarity TEXT NOT NULL DEFAULT 'common',
			price_cents INT NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
			currency TEXT NOT NULL DEFAULT 'USD',
			is_free BOOLEAN NOT NULL DEFAULT false,
			is_purchasable BOOLEAN NOT NULL DEFAULT false,
			is_active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (slot, asset_key)
		);
		CREATE TABLE cosmetic_entitlements (
			bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
			source TEXT NOT NULL DEFAULT 'manual',
			external_reference TEXT,
			granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, cosmetic_id)
		);
		CREATE UNIQUE INDEX idx_cosmetic_entitlements_external
			ON cosmetic_entitlements (source, external_reference)
			WHERE external_reference IS NOT NULL AND external_reference <> '';
		CREATE TABLE bot_cosmetic_loadout (
			bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			slot TEXT NOT NULL CHECK (slot IN ('bot_skin', 'weapon_skin', 'attachment')),
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, slot)
		);
		INSERT INTO api_keys (id) VALUES ('old-key');
		INSERT INTO bots (id, api_key_id) VALUES ('old-bot', 'old-key');
		INSERT INTO cosmetic_items
			(id, name, description, slot, asset_key, rarity, price_cents, currency, is_free, is_purchasable, is_active)
		VALUES ('skin-neon-grid', 'Neon Grid', '', 'bot_skin', 'neon_grid', 'rare', 499, 'USD', false, false, true);
		INSERT INTO cosmetic_entitlements (bot_id, cosmetic_id, source, external_reference)
		VALUES ('old-bot', 'skin-neon-grid', 'stripe', 'old-order-line-1');
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id)
		VALUES ('old-bot', 'bot_skin', 'skin-neon-grid')`)
	if err != nil {
		t.Fatalf("create PR #69 schema: %v", err)
	}

	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("upgrade PR #69 schema: %v", err)
	}
	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("repeat upgraded schema: %v", err)
	}
	if err := EnsureCosmeticSubscriptionsSchema(ctx); err != nil {
		t.Fatalf("install subscription schema for W1b.4: %v", err)
	}
	if err := EnsureCosmeticAdminMembershipsSchema(ctx); err != nil {
		t.Fatalf("install membership schema for W1b.4: %v", err)
	}
	// W1b.4 lands after the core bot timestamp migration. The fixture above is
	// intentionally the older PR #69 cosmetics shape, so add only the
	// intervening non-cosmetics columns before exercising the platform cutover.
	if _, err := Pool.Exec(ctx, `
		ALTER TABLE bots ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
		ALTER TABLE bots ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`); err != nil {
		t.Fatalf("install intervening bot timestamps: %v", err)
	}
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		t.Fatalf("complete W1b.4 authority upgrade: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id)
		VALUES ('old-bot', 'trail', 'trail-standard')`); err != nil {
		t.Fatalf("upgraded loadout constraint rejected trail slot: %v", err)
	}
	var migratedTrailItem string
	if err := Pool.QueryRow(ctx, `
		SELECT cosmetic_id FROM bot_cosmetic_loadout
		WHERE bot_id = 'old-bot' AND slot = 'trail'`).Scan(&migratedTrailItem); err != nil {
		t.Fatalf("load upgraded trail slot: %v", err)
	}
	if migratedTrailItem != "trail-standard" {
		t.Fatalf("upgraded trail slot item = %q, want trail-standard", migratedTrailItem)
	}

	var licenseID string
	if err := Pool.QueryRow(ctx, `
		SELECT id FROM cosmetic_licenses
		WHERE legacy_bot_id = 'old-bot' AND external_reference = 'old-order-line-1'`).Scan(&licenseID); err != nil {
		t.Fatalf("load upgraded legacy license: %v", err)
	}
	var migratedLoadoutLicense *string
	if err := Pool.QueryRow(ctx, `
		SELECT license_id FROM bot_cosmetic_loadout WHERE bot_id = 'old-bot' AND slot = 'bot_skin'`).Scan(&migratedLoadoutLicense); err != nil {
		t.Fatalf("load upgraded loadout: %v", err)
	}
	if migratedLoadoutLicense == nil || *migratedLoadoutLicense != licenseID {
		t.Fatalf("upgraded loadout license = %v, want %s", migratedLoadoutLicense, licenseID)
	}
	if _, err := Pool.Exec(ctx, `
		UPDATE cosmetic_licenses SET status = 'chargeback', assigned_bot_id = NULL WHERE id = $1`, licenseID); err != nil {
		t.Fatalf("mark terminal legacy license: %v", err)
	}
	if _, err := GrantCosmeticEntitlement(ctx, "old-bot", "skin-neon-grid", "manual", ""); err != nil {
		t.Fatalf("replay legacy grant: %v", err)
	}
	var terminalStatus string
	var terminalAssignment *string
	if err := Pool.QueryRow(ctx, `SELECT status, assigned_bot_id FROM cosmetic_licenses WHERE id = $1`, licenseID).
		Scan(&terminalStatus, &terminalAssignment); err != nil {
		t.Fatal(err)
	}
	if terminalStatus != "chargeback" || terminalAssignment != nil {
		t.Fatalf("legacy replay changed terminal license: status=%q assignment=%v", terminalStatus, terminalAssignment)
	}
	// A separate active legacy generation exercises account recovery. The
	// terminal copy above must remain terminal; W1b.4 deliberately forbids
	// restoring it to active for test setup or runtime recovery.
	if created, err := GrantCosmeticEntitlement(ctx, "old-bot", "weapon-solar-flare", "stripe", "old-order-line-2"); err != nil || !created {
		t.Fatalf("create active legacy recovery generation = (%v, %v)", created, err)
	}
	if _, err := Pool.Exec(ctx, `DELETE FROM api_keys WHERE id = 'old-key'`); err != nil {
		t.Fatalf("delete lost legacy key: %v", err)
	}
	recovered, claimed, err := GrantCosmeticLicense(ctx, "recovered@example.com", "weapon-solar-flare", "stripe", "old-order-line-2")
	if err != nil || !claimed || recovered.ID == licenseID || recovered.AccountID == nil || recovered.LegacyBotID != nil {
		t.Fatalf("recover legacy purchase by email/reference = (%+v, %v, %v)", recovered, claimed, err)
	}

	if _, revoked, err := RevokeCosmeticLicense(ctx, recovered.ID); err != nil || !revoked {
		t.Fatalf("revoke upgraded legacy license = (%v, %v)", revoked, err)
	}
	var remaining int
	if err := Pool.QueryRow(ctx, `SELECT COUNT(*) FROM bot_cosmetic_loadout WHERE license_id = $1`, recovered.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("legacy paid loadout rows after revoke = %d, want 0", remaining)
	}
}

func TestPostgresLegacyBotEntitlementMigratesAndSurvivesBotDeletion(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	bot := createCustomerCosmeticsTestBot(t, ctx, "legacy")

	// Recreate the exact pre-account PR #69 storage shape, then rerun startup
	// schema repair to materialize the durable legacy license.
	if _, err := Pool.Exec(ctx, `
		INSERT INTO cosmetic_entitlements (bot_id, cosmetic_id, source, external_reference)
		VALUES ($1, 'skin-neon-grid', 'legacy-test', 'legacy-order')`, bot.ID); err != nil {
		t.Fatalf("insert legacy entitlement: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id)
		VALUES ($1, 'bot_skin', 'skin-neon-grid')`, bot.ID); err != nil {
		t.Fatalf("insert legacy loadout: %v", err)
	}
	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("EnsureCosmeticsSchema legacy migration: %v", err)
	}

	var licenseID string
	var owner, legacyBot, assignedBot *string
	if err := Pool.QueryRow(ctx, `
		SELECT id, account_id, legacy_bot_id, assigned_bot_id
		FROM cosmetic_licenses WHERE external_reference = 'legacy-order'`).
		Scan(&licenseID, &owner, &legacyBot, &assignedBot); err != nil {
		t.Fatalf("load migrated legacy license: %v", err)
	}
	if owner != nil || legacyBot == nil || *legacyBot != bot.ID || assignedBot == nil || *assignedBot != bot.ID {
		t.Fatalf("migrated legacy state owner=%v legacy=%v assigned=%v", owner, legacyBot, assignedBot)
	}

	account, err := UpsertVerifiedCustomerAccount(ctx, "legacy@example.com", "https://id.example", "legacy-subject", "Legacy Owner")
	if err != nil {
		t.Fatalf("create legacy owner: %v", err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, account.ID, bot.ID); err != nil {
		t.Fatalf("claim legacy bot: %v", err)
	}
	claimed, err := getCosmeticLicense(ctx, licenseID)
	if err != nil || claimed.AccountID == nil || *claimed.AccountID != account.ID || claimed.LegacyBotID != nil ||
		claimed.AssignedBotID == nil || *claimed.AssignedBotID != bot.ID || !claimed.Equipped {
		t.Fatalf("claimed legacy license = (%+v, %v)", claimed, err)
	}

	// Even destructive stale-key cleanup can remove the bot/link/assignment but
	// never the purchased account license.
	if _, err := Pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, bot.APIKeyID); err != nil {
		t.Fatalf("delete stale API key: %v", err)
	}
	claimed, err = getCosmeticLicense(ctx, licenseID)
	if err != nil || claimed.AccountID == nil || *claimed.AccountID != account.ID || claimed.AssignedBotID != nil {
		t.Fatalf("license after bot deletion = (%+v, %v)", claimed, err)
	}

	// If stale cleanup deleted the bot before it could be linked, replaying the
	// trusted fulfillment reference claims the exact orphaned legacy copy for
	// the purchase email instead of minting a duplicate.
	orphanBot := createCustomerCosmeticsTestBot(t, ctx, "legacy-orphan")
	if _, err := Pool.Exec(ctx, `
		INSERT INTO cosmetic_entitlements (bot_id, cosmetic_id, source, external_reference)
		VALUES ($1, 'skin-neon-grid', 'legacy-test', 'legacy-orphan-order')`, orphanBot.ID); err != nil {
		t.Fatalf("insert orphan legacy entitlement: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id)
		VALUES ($1, 'bot_skin', 'skin-neon-grid')`, orphanBot.ID); err != nil {
		t.Fatalf("insert orphan legacy loadout: %v", err)
	}
	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("migrate orphan legacy entitlement: %v", err)
	}
	var orphanLicenseID string
	if err := Pool.QueryRow(ctx, `
		SELECT id FROM cosmetic_licenses
		WHERE source = 'legacy-test' AND external_reference = 'legacy-orphan-order'`).Scan(&orphanLicenseID); err != nil {
		t.Fatalf("load orphan legacy license: %v", err)
	}
	if _, err := Pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, orphanBot.APIKeyID); err != nil {
		t.Fatalf("delete orphan bot key: %v", err)
	}
	recovered, newlyClaimed, err := GrantCosmeticLicense(ctx, account.Email, "skin-neon-grid", "legacy-test", "legacy-orphan-order")
	if err != nil || !newlyClaimed || recovered.ID != orphanLicenseID ||
		recovered.AccountID == nil || *recovered.AccountID != account.ID || recovered.LegacyBotID != nil || recovered.AssignedBotID != nil {
		t.Fatalf("recover orphan legacy license = (%+v, %v, %v), want ID %s", recovered, newlyClaimed, err, orphanLicenseID)
	}
	replayed, newlyClaimed, err := GrantCosmeticLicense(ctx, account.Email, "skin-neon-grid", "legacy-test", "legacy-orphan-order")
	if err != nil || newlyClaimed || replayed.ID != orphanLicenseID {
		t.Fatalf("idempotent orphan recovery = (%+v, %v, %v)", replayed, newlyClaimed, err)
	}
}

func TestPostgresConcurrentCosmeticsSchemaRepairIsSerialized(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	const workers = 8
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			results <- EnsureCosmeticsSchema(context.Background())
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent EnsureCosmeticsSchema: %v", err)
		}
	}
	var constraintCount int
	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conrelid = 'bot_cosmetic_loadout'::regclass
		  AND conname = 'bot_cosmetic_loadout_assignment_fk'`).Scan(&constraintCount); err != nil {
		t.Fatal(err)
	}
	if constraintCount != 1 {
		t.Fatalf("assignment FK count = %d, want 1", constraintCount)
	}
}

func TestPostgresAccountRowIsFirstAssignmentLock(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	bot := createCustomerCosmeticsTestBot(t, ctx, "lock-order")
	account, err := UpsertVerifiedCustomerAccount(ctx, "locks@example.com", "https://id.example", "locks-subject", "Locks")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, account.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	license, _, err := GrantCosmeticLicense(ctx, account.Email, "skin-neon-grid", "manual", "lock-order")
	if err != nil {
		t.Fatal(err)
	}

	locker, err := Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(context.Background())
	if _, err := locker.Exec(ctx, `SELECT 1 FROM customer_accounts WHERE id = $1 FOR UPDATE`, account.ID); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := AssignCosmeticLicense(context.Background(), account.ID, license.ID, &bot.ID)
		result <- err
	}()
	waitForBlockedCustomerStatement(t, ctx, "customer_accounts")

	probe, err := Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec(ctx, `SELECT 1 FROM cosmetic_licenses WHERE id = $1 FOR UPDATE NOWAIT`, license.ID); err != nil {
		probe.Rollback(ctx)
		t.Fatalf("assignment locked license before account row: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("assignment after account unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("assignment did not finish after account unlock")
	}
}

func waitForBlockedCustomerStatement(t *testing.T, ctx context.Context, queryFragment string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		pattern := fmt.Sprintf("%%%s%%", queryFragment)
		if err := Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
				  AND wait_event_type = 'Lock'
				  AND query LIKE $1
			)`, pattern).Scan(&waiting); err != nil {
			t.Fatalf("inspect blocked customer statement: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for blocked statement containing %q", queryFragment)
}
