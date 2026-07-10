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
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
			source TEXT NOT NULL DEFAULT 'manual',
			external_reference TEXT,
			granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, cosmetic_id)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_entitlements_external
			ON cosmetic_entitlements (source, external_reference)
			WHERE external_reference IS NOT NULL AND external_reference <> ''`,
		`CREATE TABLE IF NOT EXISTS bot_cosmetic_loadout (
			bot_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			slot TEXT NOT NULL CHECK (slot IN ('bot_skin', 'weapon_skin', 'attachment')),
			cosmetic_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE CASCADE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (bot_id, slot)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_cosmetic_loadout_item ON bot_cosmetic_loadout (cosmetic_id)`,
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
		       (i.is_free OR e.bot_id IS NOT NULL) AS owned,
		       CASE
		         WHEN l.cosmetic_id IS NOT NULL THEN l.cosmetic_id = i.id
		         ELSE (i.slot = 'bot_skin' AND i.asset_key = 'standard')
		           OR (i.slot = 'weapon_skin' AND i.asset_key = 'standard')
		           OR (i.slot = 'attachment' AND i.asset_key = 'none')
		       END AS equipped
		FROM cosmetic_items i
		LEFT JOIN cosmetic_entitlements e ON e.cosmetic_id = i.id AND e.bot_id = $1
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
		WHERE l.bot_id = $1 AND i.is_active = true`, botID)
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

	if !item.IsFree {
		var entitlementMarker int
		err := tx.QueryRow(ctx, `
			SELECT 1 FROM cosmetic_entitlements
			WHERE bot_id = $1 AND cosmetic_id = $2
			FOR UPDATE`, botID, cosmeticID).Scan(&entitlementMarker)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticNotOwned
		}
		if err != nil {
			return nil, fmt.Errorf("EquipCosmetic entitlement: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO bot_cosmetic_loadout (bot_id, slot, cosmetic_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (bot_id, slot) DO UPDATE
		SET cosmetic_id = EXCLUDED.cosmetic_id, updated_at = NOW()`, botID, slot, cosmeticID); err != nil {
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

	loadoutTag, err := tx.Exec(ctx, `
		DELETE FROM bot_cosmetic_loadout
		WHERE bot_id = $1 AND cosmetic_id = $2`, botID, cosmeticID)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement loadout: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM cosmetic_entitlements
		WHERE bot_id = $1 AND cosmetic_id = $2`, botID, cosmeticID)
	if err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("RevokeCosmeticEntitlement commit: %w", err)
	}
	return loadoutTag.RowsAffected() > 0 || tag.RowsAffected() > 0, nil
}
