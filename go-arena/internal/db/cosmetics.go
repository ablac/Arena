package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	CosmeticSlotBotSkin    = "bot_skin"
	CosmeticSlotWeaponSkin = "weapon_skin"
	CosmeticSlotAttachment = "attachment"
)

var (
	ErrInvalidCosmeticSlot   = errors.New("invalid cosmetic slot")
	ErrCosmeticNotFound      = errors.New("cosmetic not found")
	ErrCosmeticInactive      = errors.New("cosmetic is not active")
	ErrCosmeticSlotMismatch  = errors.New("cosmetic does not belong to that slot")
	ErrCosmeticNotOwned      = errors.New("cosmetic is not owned by this bot")
	ErrCosmeticBotNotFound   = errors.New("bot not found")
	ErrCosmeticGrantConflict = errors.New("external reference already granted a different cosmetic")
)

// CosmeticItem is intentionally presentation-only. No gameplay stat, weapon
// damage, cooldown, movement, or loadout field is stored in the cosmetics
// schema, keeping paid ownership out of the game-mechanics boundary.
type CosmeticItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Slot          string `json:"slot"`
	AssetKey      string `json:"asset_key"`
	Rarity        string `json:"rarity"`
	PriceCents    int    `json:"price_cents"`
	Currency      string `json:"currency"`
	IsFree        bool   `json:"is_free"`
	IsPurchasable bool   `json:"is_purchasable"`
	IsActive      bool   `json:"is_active"`
}

// BotCosmeticItem adds ownership and equip state for an authenticated bot.
type BotCosmeticItem struct {
	CosmeticItem
	Owned    bool `json:"owned"`
	Equipped bool `json:"equipped"`
}

var starterCosmeticCatalog = []CosmeticItem{
	{
		ID: "skin-standard", Name: "Standard Chassis", Description: "The classic Arena bot finish.",
		Slot: CosmeticSlotBotSkin, AssetKey: "standard", Rarity: "common", Currency: "USD", IsFree: true, IsActive: true,
	},
	{
		ID: "skin-neon-grid", Name: "Neon Grid Chassis", Description: "Animated-looking neon armor accents with no gameplay effect.",
		Slot: CosmeticSlotBotSkin, AssetKey: "neon_grid", Rarity: "rare", PriceCents: 499, Currency: "USD", IsActive: true,
	},
	{
		ID: "skin-carbon-armor", Name: "Carbon Armor Chassis", Description: "Dark geometric armor plates with no defensive bonus.",
		Slot: CosmeticSlotBotSkin, AssetKey: "carbon_armor", Rarity: "rare", PriceCents: 399, Currency: "USD", IsActive: true,
	},
	{
		ID: "weapon-standard", Name: "Standard Weapon Finish", Description: "The default Arena weapon materials.",
		Slot: CosmeticSlotWeaponSkin, AssetKey: "standard", Rarity: "common", Currency: "USD", IsFree: true, IsActive: true,
	},
	{
		ID: "weapon-solar-flare", Name: "Solar Flare Finish", Description: "A warm gold weapon finish with no damage bonus.",
		Slot: CosmeticSlotWeaponSkin, AssetKey: "solar_flare", Rarity: "rare", PriceCents: 299, Currency: "USD", IsActive: true,
	},
	{
		ID: "weapon-void-edge", Name: "Void Edge Finish", Description: "A violet-black weapon finish with no cooldown bonus.",
		Slot: CosmeticSlotWeaponSkin, AssetKey: "void_edge", Rarity: "rare", PriceCents: 299, Currency: "USD", IsActive: true,
	},
	{
		ID: "attachment-none", Name: "No Attachment", Description: "Run the chassis without an attachment.",
		Slot: CosmeticSlotAttachment, AssetKey: "none", Rarity: "common", Currency: "USD", IsFree: true, IsActive: true,
	},
	{
		ID: "attachment-signal-antenna", Name: "Signal Antenna", Description: "A free starter antenna for proving the customization flow end to end.",
		Slot: CosmeticSlotAttachment, AssetKey: "signal_antenna", Rarity: "common", Currency: "USD", IsFree: true, IsActive: true,
	},
	{
		ID: "attachment-orbital-halo", Name: "Orbital Halo", Description: "A luminous halo attachment with no gameplay effect.",
		Slot: CosmeticSlotAttachment, AssetKey: "orbital_halo", Rarity: "epic", PriceCents: 299, Currency: "USD", IsActive: true,
	},
}

// DefaultCosmeticCatalog returns a copy so callers cannot mutate the seed
// catalog shared by schema setup and DB-unavailable public responses.
func DefaultCosmeticCatalog() []CosmeticItem {
	items := make([]CosmeticItem, len(starterCosmeticCatalog))
	copy(items, starterCosmeticCatalog)
	return items
}

func IsValidCosmeticSlot(slot string) bool {
	switch strings.TrimSpace(strings.ToLower(slot)) {
	case CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment:
		return true
	default:
		return false
	}
}

// EnsureCosmeticsSchema creates the provider-neutral catalog, entitlement,
// and equip tables. Payment processors only grant entitlements; the game
// engine consumes the resulting visual asset keys and never price metadata.
func EnsureCosmeticsSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsureCosmeticsSchema begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Startup can race across multiple arena-server replicas. PostgreSQL DDL is
	// transactional, but IF NOT EXISTS plus follow-up ALTER/migration statements
	// still need one schema owner at a time to avoid duplicate-constraint races.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026071001::BIGINT)`); err != nil {
		return fmt.Errorf("EnsureCosmeticsSchema migration lock: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS cosmetic_items (
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
		)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_entitlements (
			bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE RESTRICT,
			source TEXT NOT NULL DEFAULT 'manual',
			external_reference TEXT,
			granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, cosmetic_id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_entitlements_external
			ON cosmetic_entitlements (source, external_reference)
			WHERE external_reference IS NOT NULL AND external_reference <> ''`,
		`CREATE TABLE IF NOT EXISTS customer_accounts (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE CHECK (email = LOWER(email)),
			display_name TEXT NOT NULL DEFAULT '',
			email_verified_at TIMESTAMPTZ,
			oidc_issuer TEXT,
			oidc_subject TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT customer_accounts_oidc_pair_check CHECK (
				(oidc_issuer IS NULL AND oidc_subject IS NULL) OR
				(oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL)
			)
		)`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'customer_accounts'::regclass
				  AND conname = 'customer_accounts_oidc_pair_check'
			) THEN
				ALTER TABLE customer_accounts
					ADD CONSTRAINT customer_accounts_oidc_pair_check CHECK (
						(oidc_issuer IS NULL AND oidc_subject IS NULL) OR
						(oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL)
					);
			END IF;
		END
		$$`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_accounts_oidc_identity
			ON customer_accounts (oidc_issuer, oidc_subject)
			WHERE oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS account_bot_links (
			account_id TEXT NOT NULL REFERENCES customer_accounts(id) ON DELETE CASCADE,
			bot_id TEXT NOT NULL UNIQUE REFERENCES bots(id) ON DELETE CASCADE,
			linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (account_id, bot_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_account_bot_links_account
			ON account_bot_links (account_id, linked_at, bot_id)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_licenses (
			id TEXT PRIMARY KEY,
			account_id TEXT REFERENCES customer_accounts(id) ON DELETE RESTRICT,
			legacy_bot_id TEXT,
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE RESTRICT,
			assigned_bot_id TEXT REFERENCES bots(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'active'
				CHECK (status IN ('active', 'refunded', 'revoked', 'chargeback')),
			source TEXT NOT NULL DEFAULT 'manual',
			external_reference TEXT,
			granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (id, account_id),
			CHECK (
				(account_id IS NOT NULL AND legacy_bot_id IS NULL AND assigned_bot_id IS NULL) OR
				(account_id IS NULL AND legacy_bot_id IS NOT NULL)
			)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_licenses_external
			ON cosmetic_licenses (source, external_reference)
			WHERE external_reference IS NOT NULL AND external_reference <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_licenses_account
			ON cosmetic_licenses (account_id, granted_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_licenses_assignment
			ON cosmetic_licenses (assigned_bot_id, cosmetic_id)
			WHERE assigned_bot_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS cosmetic_license_assignments (
			license_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			bot_id TEXT NOT NULL,
			assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (license_id, account_id, bot_id),
			FOREIGN KEY (license_id, account_id)
				REFERENCES cosmetic_licenses(id, account_id) ON DELETE CASCADE,
			FOREIGN KEY (account_id, bot_id)
				REFERENCES account_bot_links(account_id, bot_id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_license_assignments_bot
			ON cosmetic_license_assignments (account_id, bot_id, assigned_at)`,
		`CREATE TABLE IF NOT EXISTS bot_cosmetic_loadout (
			bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			slot TEXT NOT NULL CHECK (slot IN ('bot_skin', 'weapon_skin', 'attachment')),
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
			license_id TEXT REFERENCES cosmetic_licenses(id) ON DELETE CASCADE,
			account_id TEXT REFERENCES customer_accounts(id) ON DELETE RESTRICT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, slot),
			CONSTRAINT bot_cosmetic_loadout_assignment_fk
			FOREIGN KEY (license_id, account_id, bot_id)
				REFERENCES cosmetic_license_assignments(license_id, account_id, bot_id) ON DELETE CASCADE
		)`,
		`ALTER TABLE bot_cosmetic_loadout
			ADD COLUMN IF NOT EXISTS license_id TEXT REFERENCES cosmetic_licenses(id) ON DELETE CASCADE`,
		`ALTER TABLE bot_cosmetic_loadout
			ADD COLUMN IF NOT EXISTS account_id TEXT REFERENCES customer_accounts(id) ON DELETE RESTRICT`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'bot_cosmetic_loadout'::regclass
				  AND conname = 'bot_cosmetic_loadout_assignment_fk'
			) THEN
				ALTER TABLE bot_cosmetic_loadout
					ADD CONSTRAINT bot_cosmetic_loadout_assignment_fk
					FOREIGN KEY (license_id, account_id, bot_id)
					REFERENCES cosmetic_license_assignments(license_id, account_id, bot_id)
					ON DELETE CASCADE;
			END IF;
		END
		$$`,
		`CREATE INDEX IF NOT EXISTS idx_bot_cosmetic_loadout_item ON bot_cosmetic_loadout (cosmetic_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_bot_cosmetic_loadout_license
			ON bot_cosmetic_loadout (license_id) WHERE license_id IS NOT NULL`,
	}

	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("EnsureCosmeticsSchema exec: %w", err)
		}
	}

	for _, item := range starterCosmeticCatalog {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cosmetic_items
				(id, name, description, slot, asset_key, rarity, price_cents, currency, is_free, is_purchasable, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (id) DO NOTHING`,
			item.ID, item.Name, item.Description, item.Slot, item.AssetKey, item.Rarity,
			item.PriceCents, item.Currency, item.IsFree, item.IsPurchasable, item.IsActive,
		); err != nil {
			return fmt.Errorf("EnsureCosmeticsSchema seed %s: %w", item.ID, err)
		}
	}

	// Existing deployments stored paid ownership directly against a bot. Keep
	// those rows intact as a rollback/audit source, while materialising one
	// stable legacy license per entitlement. The first verified account that
	// proves possession of that bot's API key claims the license atomically.
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_licenses
			(id, account_id, legacy_bot_id, cosmetic_id, assigned_bot_id, source, external_reference, granted_at, updated_at)
		SELECT 'legacy-' || MD5(e.bot_id || CHR(31) || e.cosmetic_id),
		       NULL, e.bot_id, e.cosmetic_id, e.bot_id, e.source, e.external_reference, e.granted_at, NOW()
		FROM cosmetic_entitlements e
		ON CONFLICT DO NOTHING`); err != nil {
		return fmt.Errorf("EnsureCosmeticsSchema migrate legacy licenses: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE bot_cosmetic_loadout l
		SET license_id = cl.id, updated_at = NOW()
		FROM cosmetic_licenses cl
		JOIN cosmetic_items i ON i.id = cl.cosmetic_id
		WHERE l.license_id IS NULL
		  AND i.is_free = false
		  AND cl.legacy_bot_id = l.bot_id
		  AND cl.assigned_bot_id = l.bot_id
		  AND cl.cosmetic_id = l.cosmetic_id`); err != nil {
		return fmt.Errorf("EnsureCosmeticsSchema migrate legacy loadouts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureCosmeticsSchema commit: %w", err)
	}
	return nil
}

func ListCosmeticCatalog(ctx context.Context) ([]CosmeticItem, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT id, name, description, slot, asset_key, rarity, price_cents, currency,
		       is_free, is_purchasable, is_active
		FROM cosmetic_items
		WHERE is_active = true
		ORDER BY CASE slot WHEN 'bot_skin' THEN 1 WHEN 'weapon_skin' THEN 2 ELSE 3 END,
		         price_cents, name`)
	if err != nil {
		return nil, fmt.Errorf("ListCosmeticCatalog: %w", err)
	}
	defer rows.Close()

	items := make([]CosmeticItem, 0)
	for rows.Next() {
		var item CosmeticItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Slot, &item.AssetKey,
			&item.Rarity, &item.PriceCents, &item.Currency, &item.IsFree, &item.IsPurchasable, &item.IsActive); err != nil {
			return nil, fmt.Errorf("ListCosmeticCatalog scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ListBotCosmetics(ctx context.Context, botID string) ([]BotCosmeticItem, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT i.id, i.name, i.description, i.slot, i.asset_key, i.rarity,
		       i.price_cents, i.currency, i.is_free, i.is_purchasable, i.is_active,
		       (i.is_free OR EXISTS (
		         SELECT 1 FROM cosmetic_licenses owned_license
		         WHERE owned_license.cosmetic_id = i.id
		           AND owned_license.status = 'active'
		           AND (
		             owned_license.assigned_bot_id = $1 OR EXISTS (
		               SELECT 1 FROM cosmetic_license_assignments owned_assignment
		               WHERE owned_assignment.license_id = owned_license.id
		                 AND owned_assignment.bot_id = $1
		             )
		           )
		       )) AS owned,
		       CASE
		         WHEN l.cosmetic_id IS NOT NULL THEN l.cosmetic_id = i.id
		         ELSE (i.slot = 'bot_skin' AND i.asset_key = 'standard')
		           OR (i.slot = 'weapon_skin' AND i.asset_key = 'standard')
		           OR (i.slot = 'attachment' AND i.asset_key = 'none')
		       END AS equipped
		FROM cosmetic_items i
		LEFT JOIN bot_cosmetic_loadout l ON l.bot_id = $1 AND l.slot = i.slot
		  AND EXISTS (
		    SELECT 1 FROM cosmetic_items equipped_item
		    WHERE equipped_item.id = l.cosmetic_id AND equipped_item.is_active = true
		  )
		WHERE i.is_active = true
		ORDER BY CASE i.slot WHEN 'bot_skin' THEN 1 WHEN 'weapon_skin' THEN 2 ELSE 3 END,
		         i.price_cents, i.name`, botID)
	if err != nil {
		return nil, fmt.Errorf("ListBotCosmetics: %w", err)
	}
	defer rows.Close()

	items := make([]BotCosmeticItem, 0)
	for rows.Next() {
		var item BotCosmeticItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Slot, &item.AssetKey,
			&item.Rarity, &item.PriceCents, &item.Currency, &item.IsFree, &item.IsPurchasable,
			&item.IsActive, &item.Owned, &item.Equipped); err != nil {
			return nil, fmt.Errorf("ListBotCosmetics scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetEquippedCosmetics returns only allowlisted asset identifiers for the
// spectator protocol. Price and entitlement metadata never enters BotState.
func GetEquippedCosmetics(ctx context.Context, botID string) (map[string]string, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT l.slot, i.asset_key
		FROM bot_cosmetic_loadout l
		JOIN cosmetic_items i ON i.id = l.cosmetic_id AND i.slot = l.slot
		LEFT JOIN cosmetic_licenses cl ON cl.id = l.license_id
		LEFT JOIN cosmetic_license_assignments cla
		  ON cla.license_id = l.license_id AND cla.bot_id = l.bot_id AND cla.account_id = l.account_id
		WHERE l.bot_id = $1 AND i.is_active = true
		  AND (
		    i.is_free = true OR
		    (cl.id IS NOT NULL AND cl.cosmetic_id = i.id AND cl.status = 'active' AND (
		      (cl.account_id IS NULL AND cl.assigned_bot_id = l.bot_id) OR
		      (cl.account_id IS NOT NULL AND cla.license_id IS NOT NULL)
		    ))
		  )`, botID)
	if err != nil {
		return nil, fmt.Errorf("GetEquippedCosmetics: %w", err)
	}
	defer rows.Close()

	result := map[string]string{
		CosmeticSlotBotSkin:    "standard",
		CosmeticSlotWeaponSkin: "standard",
		CosmeticSlotAttachment: "none",
	}
	for rows.Next() {
		var slot, assetKey string
		if err := rows.Scan(&slot, &assetKey); err != nil {
			return nil, fmt.Errorf("GetEquippedCosmetics scan: %w", err)
		}
		if IsValidCosmeticSlot(slot) {
			result[slot] = assetKey
		}
	}
	return result, rows.Err()
}

func EquipCosmetic(ctx context.Context, botID, slot, cosmeticID string) (*CosmeticItem, error) {
	slot = strings.TrimSpace(strings.ToLower(slot))
	if !IsValidCosmeticSlot(slot) {
		return nil, ErrInvalidCosmeticSlot
	}
	if Pool == nil {
		return nil, ErrNoDatabase
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("EquipCosmetic begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var item CosmeticItem
	err = tx.QueryRow(ctx, `
		SELECT id, name, description, slot, asset_key, rarity, price_cents, currency,
		       is_free, is_purchasable, is_active
		FROM cosmetic_items WHERE id = $1`, cosmeticID).
		Scan(&item.ID, &item.Name, &item.Description, &item.Slot, &item.AssetKey, &item.Rarity,
			&item.PriceCents, &item.Currency, &item.IsFree, &item.IsPurchasable, &item.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCosmeticNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("EquipCosmetic item: %w", err)
	}
	if !item.IsActive {
		return nil, ErrCosmeticInactive
	}
	if item.Slot != slot {
		return nil, ErrCosmeticSlotMismatch
	}

	var licenseID, licenseAccountID *string
	if !item.IsFree {
		var assignedLicenseID string
		var assignedAccountID *string
		// Discover the candidate without taking a subordinate lock. Account-owned
		// equip must lock the account row before it locks the exact license so it
		// cannot deadlock with dashboard assign/unlink operations.
		err := tx.QueryRow(ctx, `
			SELECT cl.id, cl.account_id
			FROM cosmetic_licenses cl
			LEFT JOIN cosmetic_license_assignments cla ON cla.license_id = cl.id
			WHERE cl.cosmetic_id = $2 AND cl.status = 'active'
			  AND (cl.assigned_bot_id = $1 OR cla.bot_id = $1)
			ORDER BY cl.granted_at, cl.id
			LIMIT 1`, botID, cosmeticID).Scan(&assignedLicenseID, &assignedAccountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticNotOwned
		}
		if err != nil {
			return nil, fmt.Errorf("EquipCosmetic license candidate: %w", err)
		}
		if assignedAccountID != nil {
			if _, err := lockCustomerAccount(ctx, tx, *assignedAccountID, true); err != nil {
				return nil, err
			}
		}
		var lockedAccountID *string
		err = tx.QueryRow(ctx, `
			SELECT cl.account_id
			FROM cosmetic_licenses cl
			LEFT JOIN cosmetic_license_assignments cla ON cla.license_id = cl.id
			WHERE cl.id = $1 AND cl.cosmetic_id = $2 AND cl.status = 'active'
			  AND (cl.assigned_bot_id = $3 OR cla.bot_id = $3)
			FOR UPDATE OF cl`, assignedLicenseID, cosmeticID, botID).Scan(&lockedAccountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticNotOwned
		}
		if err != nil {
			return nil, fmt.Errorf("EquipCosmetic license lock: %w", err)
		}
		if (assignedAccountID == nil) != (lockedAccountID == nil) ||
			(assignedAccountID != nil && lockedAccountID != nil && *assignedAccountID != *lockedAccountID) {
			return nil, ErrCosmeticNotOwned
		}
		licenseID = &assignedLicenseID
		licenseAccountID = lockedAccountID
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id, license_id, account_id, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (bot_id, slot) DO UPDATE
		SET cosmetic_id = EXCLUDED.cosmetic_id, license_id = EXCLUDED.license_id,
		    account_id = EXCLUDED.account_id, updated_at = NOW()`,
		botID, slot, cosmeticID, licenseID, licenseAccountID); err != nil {
		return nil, fmt.Errorf("EquipCosmetic upsert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("EquipCosmetic commit: %w", err)
	}
	return &item, nil
}

// GrantCosmeticEntitlement is the provider-neutral fulfillment seam. Stripe,
// Paddle, promotions, or manual support tools can all supply an idempotency
// reference without changing the game engine or equip endpoint.
func GrantCosmeticEntitlement(ctx context.Context, botID, cosmeticID, source, externalReference string) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	if source == "" {
		source = "manual"
	}

	var botExists, itemExists bool
	if err := Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bots WHERE id = $1)`, botID).Scan(&botExists); err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement bot lookup: %w", err)
	}
	if !botExists {
		return false, ErrCosmeticBotNotFound
	}
	if err := Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cosmetic_items WHERE id = $1 AND is_active = true)`, cosmeticID).Scan(&itemExists); err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement item lookup: %w", err)
	}
	if !itemExists {
		return false, ErrCosmeticNotFound
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if externalReference != "" {
		var existingBotID, existingCosmeticID string
		err := tx.QueryRow(ctx, `
			SELECT bot_id, cosmetic_id FROM cosmetic_entitlements
			WHERE source = $1 AND external_reference = $2
			FOR UPDATE`, source, externalReference).
			Scan(&existingBotID, &existingCosmeticID)
		if err == nil {
			if existingBotID == botID && existingCosmeticID == cosmeticID {
				return false, nil
			}
			return false, ErrCosmeticGrantConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("GrantCosmeticEntitlement idempotency lookup: %w", err)
		}
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_entitlements (bot_id, cosmetic_id, source, external_reference, granted_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NOW())
		ON CONFLICT DO NOTHING`, botID, cosmeticID, source, externalReference)
	if err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement: %w", err)
	}
	created := tag.RowsAffected() > 0
	if !created && externalReference != "" {
		var existingBotID, existingCosmeticID string
		if err := tx.QueryRow(ctx, `
			SELECT bot_id, cosmetic_id FROM cosmetic_entitlements
			WHERE source = $1 AND external_reference = $2`, source, externalReference).
			Scan(&existingBotID, &existingCosmeticID); err == nil &&
			(existingBotID != botID || existingCosmeticID != cosmeticID) {
			return false, ErrCosmeticGrantConflict
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_licenses
			(id, account_id, legacy_bot_id, cosmetic_id, assigned_bot_id, source, external_reference, granted_at, updated_at)
		SELECT 'legacy-' || MD5(bot_id || CHR(31) || cosmetic_id),
		       NULL, bot_id, cosmetic_id, bot_id, source, external_reference, granted_at, NOW()
		FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2
		ON CONFLICT (id) DO NOTHING`, botID, cosmeticID); err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement legacy license: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("GrantCosmeticEntitlement commit: %w", err)
	}
	return created, nil
}

func RevokeCosmeticEntitlement(ctx context.Context, botID, cosmeticID string) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Equip locks this same entitlement row before writing the loadout. Taking
	// the lock before either delete gives equip and revoke one consistent lock
	// order; otherwise revoke can delete an empty loadout, wait on entitlement,
	// and leave behind a paid loadout inserted by the concurrent equip.
	var entitlementMarker int
	err = tx.QueryRow(ctx, `
		SELECT 1 FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2
		FOR UPDATE`, botID, cosmeticID).Scan(&entitlementMarker)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("RevokeCosmeticEntitlement lock: %w", err)
	}

	// Serialize with account assignment/equip if this entitlement has not yet
	// been claimed. Claimed licenses are durable account property and are not
	// removed by this compatibility-only bot entitlement helper.
	if _, err := tx.Exec(ctx, `
		SELECT 1 FROM cosmetic_licenses
		WHERE legacy_bot_id = $1 AND cosmetic_id = $2
		FOR UPDATE`, botID, cosmeticID); err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement license lock: %w", err)
	}

	loadoutTag, err := tx.Exec(ctx, `
		DELETE FROM bot_cosmetic_loadout
		WHERE bot_id = $1 AND cosmetic_id = $2
		  AND (
		    license_id IS NULL OR license_id IN (
		      SELECT id FROM cosmetic_licenses
		      WHERE legacy_bot_id = $1 AND cosmetic_id = $2
		    )
		  )`, botID, cosmeticID)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement loadout: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2`, botID, cosmeticID)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement grant: %w", err)
	}
	licenseTag, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses
		SET status = CASE WHEN status = 'active' THEN 'revoked' ELSE status END,
		    assigned_bot_id = NULL, updated_at = NOW()
		WHERE legacy_bot_id = $1 AND cosmetic_id = $2
		  AND (status = 'active' OR assigned_bot_id IS NOT NULL)`, botID, cosmeticID)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement license: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement commit: %w", err)
	}
	return loadoutTag.RowsAffected() > 0 || tag.RowsAffected() > 0 || licenseTag.RowsAffected() > 0, nil
}
