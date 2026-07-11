package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrCosmeticCatalogInvalid   = errors.New("invalid cosmetic catalog metadata")
	ErrCosmeticCatalogConflict  = errors.New("cosmetic catalog change conflicts with existing data")
	ErrCosmeticCategoryNotFound = errors.New("cosmetic category not found")
	ErrCosmeticPackNotFound     = errors.New("cosmetic pack not found")
)

var cosmeticCatalogIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var cosmeticAssetKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var cosmeticCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var cosmeticRarityPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type CosmeticCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
}

type CosmeticPack struct {
	ID            string         `json:"id"`
	CategoryID    string         `json:"category_id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	PriceCents    int            `json:"price_cents"`
	Currency      string         `json:"currency"`
	IsFree        bool           `json:"is_free"`
	IsPurchasable bool           `json:"is_purchasable"`
	IsActive      bool           `json:"is_active"`
	SortOrder     int            `json:"sort_order"`
	ItemIDs       []string       `json:"item_ids,omitempty"`
	Items         []CosmeticItem `json:"items"`
}

type CosmeticCatalog struct {
	Categories []CosmeticCategory `json:"categories"`
	Packs      []CosmeticPack     `json:"packs"`
	Items      []CosmeticItem     `json:"items"`
}

type CosmeticCatalogAudit struct {
	ID         int64           `json:"id"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

var starterCosmeticCategories = []CosmeticCategory{
	{ID: "chassis", Name: "Chassis", Description: "Presentation-only bot body finishes.", IsActive: true, SortOrder: 10},
	{ID: "weapon-finishes", Name: "Weapon Finishes", Description: "Presentation-only weapon materials.", IsActive: true, SortOrder: 20},
	{ID: "attachments", Name: "Attachments", Description: "Presentation-only bot accessories.", IsActive: true, SortOrder: 30},
	{ID: "starter-packs", Name: "Starter Packs", Description: "Curated cosmetic bundles using Arena's built-in procedural visuals.", IsActive: true, SortOrder: 40},
}

var starterCosmeticPacks = []CosmeticPack{
	{
		ID: "neon-signal-pack", CategoryID: "starter-packs", Name: "Neon Signal Pack",
		Description: "Neon Grid chassis, Solar Flare weapon finish, and Signal Antenna.",
		PriceCents:  99, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 10,
		ItemIDs: []string{"skin-neon-grid", "weapon-solar-flare", "attachment-signal-antenna"},
	},
	{
		ID: "void-orbit-pack", CategoryID: "starter-packs", Name: "Void Orbit Pack",
		Description: "Carbon Armor chassis, Void Edge weapon finish, and Orbital Halo.",
		PriceCents:  99, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 20,
		ItemIDs: []string{"skin-carbon-armor", "weapon-void-edge", "attachment-orbital-halo"},
	},
}

func DefaultCosmeticCatalogData() CosmeticCatalog {
	items := DefaultCosmeticCatalog()
	itemByID := make(map[string]CosmeticItem, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
	}
	categories := append([]CosmeticCategory(nil), starterCosmeticCategories...)
	packs := make([]CosmeticPack, len(starterCosmeticPacks))
	for index, seed := range starterCosmeticPacks {
		pack := seed
		pack.ItemIDs = append([]string(nil), seed.ItemIDs...)
		pack.Items = make([]CosmeticItem, 0, len(seed.ItemIDs))
		for _, itemID := range seed.ItemIDs {
			if item, ok := itemByID[itemID]; ok {
				pack.Items = append(pack.Items, item)
			}
		}
		packs[index] = pack
	}
	return CosmeticCatalog{Categories: categories, Packs: packs, Items: items}
}

func ValidateCosmeticCategory(category CosmeticCategory) error {
	if !cosmeticCatalogIDPattern.MatchString(category.ID) || strings.TrimSpace(category.Name) == "" ||
		len(category.Name) > 100 || len(category.Description) > 500 || category.SortOrder < 0 || category.SortOrder > 1_000_000 {
		return ErrCosmeticCatalogInvalid
	}
	return nil
}

func ValidateCosmeticItem(item CosmeticItem) error {
	if !cosmeticCatalogIDPattern.MatchString(item.ID) || !cosmeticCatalogIDPattern.MatchString(item.CategoryID) ||
		strings.TrimSpace(item.Name) == "" || len(item.Name) > 100 || len(item.Description) > 500 ||
		!IsValidCosmeticSlot(item.Slot) || !cosmeticAssetKeyPattern.MatchString(item.AssetKey) ||
		!cosmeticRarityPattern.MatchString(item.Rarity) || !validCosmeticPrice(item.PriceCents, item.Currency, item.IsFree, item.IsPurchasable) ||
		item.SortOrder < 0 || item.SortOrder > 1_000_000 {
		return ErrCosmeticCatalogInvalid
	}
	return nil
}

func ValidateCosmeticPack(pack CosmeticPack) error {
	if !cosmeticCatalogIDPattern.MatchString(pack.ID) || !cosmeticCatalogIDPattern.MatchString(pack.CategoryID) ||
		strings.TrimSpace(pack.Name) == "" || len(pack.Name) > 100 || len(pack.Description) > 500 ||
		!validCosmeticPrice(pack.PriceCents, pack.Currency, pack.IsFree, pack.IsPurchasable) ||
		pack.SortOrder < 0 || pack.SortOrder > 1_000_000 || len(pack.ItemIDs) > 500 {
		return ErrCosmeticCatalogInvalid
	}
	seen := make(map[string]struct{}, len(pack.ItemIDs))
	for _, itemID := range pack.ItemIDs {
		if !cosmeticCatalogIDPattern.MatchString(itemID) {
			return ErrCosmeticCatalogInvalid
		}
		if _, exists := seen[itemID]; exists {
			return fmt.Errorf("%w: duplicate pack item", ErrCosmeticCatalogInvalid)
		}
		seen[itemID] = struct{}{}
	}
	if pack.IsActive && pack.IsPurchasable && len(pack.ItemIDs) == 0 {
		return fmt.Errorf("%w: active purchasable pack must contain an item", ErrCosmeticCatalogInvalid)
	}
	return nil
}

func validCosmeticPrice(priceCents int, currency string, isFree, isPurchasable bool) bool {
	if priceCents < 0 || priceCents > 1_000_000 || !cosmeticCurrencyPattern.MatchString(currency) {
		return false
	}
	if isFree {
		return priceCents == 0 && !isPurchasable
	}
	if isPurchasable {
		return priceCents > 0
	}
	return true
}

func GetPublicCosmeticCatalog(ctx context.Context) (*CosmeticCatalog, error) {
	return listCosmeticCatalogData(ctx, false)
}

func GetAdminCosmeticCatalog(ctx context.Context) (*CosmeticCatalog, error) {
	return listCosmeticCatalogData(ctx, true)
}

func listCosmeticCatalogData(ctx context.Context, includeInactive bool) (*CosmeticCatalog, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	catalog := &CosmeticCatalog{
		Categories: make([]CosmeticCategory, 0),
		Packs:      make([]CosmeticPack, 0),
		Items:      make([]CosmeticItem, 0),
	}

	categoryWhere := "WHERE is_active = true"
	itemWhere := "WHERE i.is_active = true AND c.is_active = true"
	packWhere := `WHERE p.is_active = true AND c.is_active = true
		AND EXISTS (SELECT 1 FROM cosmetic_pack_items present WHERE present.pack_id = p.id)
		AND NOT EXISTS (
			SELECT 1 FROM cosmetic_pack_items membership
			JOIN cosmetic_items member_item ON member_item.id = membership.item_id
			JOIN cosmetic_categories member_category ON member_category.id = member_item.category_id
			WHERE membership.pack_id = p.id
			  AND (member_item.is_active = false OR member_category.is_active = false)
		)`
	if includeInactive {
		categoryWhere = ""
		itemWhere = ""
		packWhere = ""
	}

	categoryRows, err := Pool.Query(ctx, `
		SELECT id, name, description, is_active, sort_order
		FROM cosmetic_categories `+categoryWhere+`
		ORDER BY sort_order, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic categories: %w", err)
	}
	for categoryRows.Next() {
		var category CosmeticCategory
		if err := categoryRows.Scan(&category.ID, &category.Name, &category.Description, &category.IsActive, &category.SortOrder); err != nil {
			categoryRows.Close()
			return nil, fmt.Errorf("scan cosmetic category: %w", err)
		}
		catalog.Categories = append(catalog.Categories, category)
	}
	if err := categoryRows.Err(); err != nil {
		categoryRows.Close()
		return nil, fmt.Errorf("iterate cosmetic categories: %w", err)
	}
	categoryRows.Close()

	itemRows, err := Pool.Query(ctx, `
		SELECT i.id, i.name, i.description, i.category_id, i.slot, i.asset_key, i.rarity,
		       i.price_cents, i.currency, i.is_free, i.is_purchasable, i.is_active, i.sort_order
		FROM cosmetic_items i
		JOIN cosmetic_categories c ON c.id = i.category_id `+itemWhere+`
		ORDER BY c.sort_order, i.sort_order, i.name, i.id`)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic items: %w", err)
	}
	for itemRows.Next() {
		var item CosmeticItem
		if err := scanCosmeticItem(itemRows, &item); err != nil {
			itemRows.Close()
			return nil, fmt.Errorf("scan cosmetic item: %w", err)
		}
		catalog.Items = append(catalog.Items, item)
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		return nil, fmt.Errorf("iterate cosmetic items: %w", err)
	}
	itemRows.Close()

	packRows, err := Pool.Query(ctx, `
		SELECT p.id, p.category_id, p.name, p.description, p.price_cents, p.currency,
		       p.is_free, p.is_purchasable, p.is_active, p.sort_order
		FROM cosmetic_packs p
		JOIN cosmetic_categories c ON c.id = p.category_id `+packWhere+`
		ORDER BY c.sort_order, p.sort_order, p.name, p.id`)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic packs: %w", err)
	}
	packIndexes := make(map[string]int)
	for packRows.Next() {
		var pack CosmeticPack
		if err := packRows.Scan(&pack.ID, &pack.CategoryID, &pack.Name, &pack.Description,
			&pack.PriceCents, &pack.Currency, &pack.IsFree, &pack.IsPurchasable, &pack.IsActive, &pack.SortOrder); err != nil {
			packRows.Close()
			return nil, fmt.Errorf("scan cosmetic pack: %w", err)
		}
		pack.ItemIDs = make([]string, 0)
		pack.Items = make([]CosmeticItem, 0)
		packIndexes[pack.ID] = len(catalog.Packs)
		catalog.Packs = append(catalog.Packs, pack)
	}
	if err := packRows.Err(); err != nil {
		packRows.Close()
		return nil, fmt.Errorf("iterate cosmetic packs: %w", err)
	}
	packRows.Close()

	membershipWhere := "WHERE i.is_active = true AND c.is_active = true"
	if includeInactive {
		membershipWhere = ""
	}
	membershipRows, err := Pool.Query(ctx, `
		SELECT pi.pack_id, i.id, i.name, i.description, i.category_id, i.slot, i.asset_key, i.rarity,
		       i.price_cents, i.currency, i.is_free, i.is_purchasable, i.is_active, i.sort_order
		FROM cosmetic_pack_items pi
		JOIN cosmetic_items i ON i.id = pi.item_id
		JOIN cosmetic_categories c ON c.id = i.category_id `+membershipWhere+`
		ORDER BY pi.pack_id, pi.sort_order, i.id`)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic pack items: %w", err)
	}
	for membershipRows.Next() {
		var packID string
		var item CosmeticItem
		if err := membershipRows.Scan(&packID, &item.ID, &item.Name, &item.Description, &item.CategoryID,
			&item.Slot, &item.AssetKey, &item.Rarity, &item.PriceCents, &item.Currency, &item.IsFree,
			&item.IsPurchasable, &item.IsActive, &item.SortOrder); err != nil {
			membershipRows.Close()
			return nil, fmt.Errorf("scan cosmetic pack item: %w", err)
		}
		if index, ok := packIndexes[packID]; ok {
			catalog.Packs[index].ItemIDs = append(catalog.Packs[index].ItemIDs, item.ID)
			catalog.Packs[index].Items = append(catalog.Packs[index].Items, item)
		}
	}
	if err := membershipRows.Err(); err != nil {
		membershipRows.Close()
		return nil, fmt.Errorf("iterate cosmetic pack items: %w", err)
	}
	membershipRows.Close()
	return catalog, nil
}

type cosmeticItemScanner interface {
	Scan(...any) error
}

func scanCosmeticItem(row cosmeticItemScanner, item *CosmeticItem) error {
	return row.Scan(&item.ID, &item.Name, &item.Description, &item.CategoryID, &item.Slot, &item.AssetKey,
		&item.Rarity, &item.PriceCents, &item.Currency, &item.IsFree, &item.IsPurchasable, &item.IsActive, &item.SortOrder)
}

func UpsertCosmeticCategory(ctx context.Context, category CosmeticCategory, actor string) (*CosmeticCategory, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if err := ValidateCosmeticCategory(category); err != nil {
		return nil, err
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert cosmetic category begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockCosmeticCatalogEntity(ctx, tx, "category", category.ID); err != nil {
		return nil, err
	}
	if err := lockAllCosmeticFallbackSlots(ctx, tx); err != nil {
		return nil, err
	}
	before, existed, err := cosmeticEntitySnapshot(ctx, tx, "category", category.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic category before update: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_categories (id, name, description, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, description = EXCLUDED.description,
			is_active = EXCLUDED.is_active, sort_order = EXCLUDED.sort_order, updated_at = NOW()`,
		category.ID, strings.TrimSpace(category.Name), strings.TrimSpace(category.Description), category.IsActive, category.SortOrder); err != nil {
		return nil, fmt.Errorf("upsert cosmetic category: %w", mapCosmeticCatalogMutationError(err))
	}
	if err := requireAllCosmeticSlotFallbacks(ctx, tx); err != nil {
		return nil, err
	}
	after, _, err := cosmeticEntitySnapshot(ctx, tx, "category", category.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic category after update: %w", err)
	}
	if err := writeCosmeticCatalogAudit(ctx, tx, catalogMutationAction(existed), "category", category.ID, actor, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("upsert cosmetic category commit: %w", err)
	}
	category.Name = strings.TrimSpace(category.Name)
	category.Description = strings.TrimSpace(category.Description)
	return &category, nil
}

func DeleteCosmeticCategory(ctx context.Context, categoryID, actor string) (bool, error) {
	return deleteCosmeticCatalogEntity(ctx, "category", categoryID, actor)
}

func UpsertCosmeticCatalogItem(ctx context.Context, item CosmeticItem, actor string) (*CosmeticItem, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if err := ValidateCosmeticItem(item); err != nil {
		return nil, err
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert cosmetic item begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockCosmeticCatalogEntity(ctx, tx, "item", item.ID); err != nil {
		return nil, err
	}
	if err := lockCosmeticFallbackSlot(ctx, tx, item.Slot); err != nil {
		return nil, err
	}
	var categoryExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cosmetic_categories WHERE id = $1)`, item.CategoryID).Scan(&categoryExists); err != nil {
		return nil, fmt.Errorf("check cosmetic item category: %w", err)
	}
	if !categoryExists {
		return nil, fmt.Errorf("%w: category does not exist", ErrCosmeticCatalogInvalid)
	}
	before, existed, err := cosmeticEntitySnapshot(ctx, tx, "item", item.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic item before update: %w", err)
	}
	if existed {
		var existingSlot, existingAssetKey string
		if err := tx.QueryRow(ctx, `SELECT slot, asset_key FROM cosmetic_items WHERE id = $1`, item.ID).Scan(&existingSlot, &existingAssetKey); err != nil {
			return nil, fmt.Errorf("read cosmetic item render identity: %w", err)
		}
		if existingSlot != item.Slot || existingAssetKey != item.AssetKey {
			return nil, fmt.Errorf("%w: slot and asset_key are immutable", ErrCosmeticCatalogConflict)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_items
			(id, name, description, category_id, slot, asset_key, rarity, price_cents, currency,
			 is_free, is_purchasable, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, description = EXCLUDED.description, category_id = EXCLUDED.category_id,
			rarity = EXCLUDED.rarity, price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency,
			is_free = EXCLUDED.is_free, is_purchasable = EXCLUDED.is_purchasable,
			is_active = EXCLUDED.is_active, sort_order = EXCLUDED.sort_order, updated_at = NOW()`,
		item.ID, strings.TrimSpace(item.Name), strings.TrimSpace(item.Description), item.CategoryID, item.Slot,
		item.AssetKey, item.Rarity, item.PriceCents, item.Currency, item.IsFree, item.IsPurchasable, item.IsActive, item.SortOrder); err != nil {
		return nil, fmt.Errorf("upsert cosmetic item: %w", mapCosmeticCatalogMutationError(err))
	}
	if err := requireCosmeticSlotFallback(ctx, tx, item.Slot); err != nil {
		return nil, err
	}
	after, _, err := cosmeticEntitySnapshot(ctx, tx, "item", item.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic item after update: %w", err)
	}
	if err := writeCosmeticCatalogAudit(ctx, tx, catalogMutationAction(existed), "item", item.ID, actor, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("upsert cosmetic item commit: %w", err)
	}
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	return &item, nil
}

func DeleteCosmeticCatalogItem(ctx context.Context, itemID, actor string) (bool, error) {
	return deleteCosmeticCatalogEntity(ctx, "item", itemID, actor)
}

func UpsertCosmeticPack(ctx context.Context, pack CosmeticPack, actor string) (*CosmeticPack, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if err := ValidateCosmeticPack(pack); err != nil {
		return nil, err
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert cosmetic pack begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockCosmeticCatalogEntity(ctx, tx, "pack", pack.ID); err != nil {
		return nil, err
	}
	var categoryExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cosmetic_categories WHERE id = $1)`, pack.CategoryID).Scan(&categoryExists); err != nil {
		return nil, fmt.Errorf("check cosmetic pack category: %w", err)
	}
	if !categoryExists {
		return nil, fmt.Errorf("%w: category does not exist", ErrCosmeticCatalogInvalid)
	}
	var itemCount int
	if len(pack.ItemIDs) > 0 {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_items WHERE id = ANY($1)`, pack.ItemIDs).Scan(&itemCount); err != nil {
			return nil, fmt.Errorf("check cosmetic pack items: %w", err)
		}
	}
	if itemCount != len(pack.ItemIDs) {
		return nil, fmt.Errorf("%w: pack contains an unknown item", ErrCosmeticCatalogInvalid)
	}
	before, existed, err := cosmeticEntitySnapshot(ctx, tx, "pack", pack.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic pack before update: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_packs
			(id, category_id, name, description, price_cents, currency, is_free, is_purchasable, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			category_id = EXCLUDED.category_id, name = EXCLUDED.name, description = EXCLUDED.description,
			price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency, is_free = EXCLUDED.is_free,
			is_purchasable = EXCLUDED.is_purchasable, is_active = EXCLUDED.is_active,
			sort_order = EXCLUDED.sort_order, updated_at = NOW()`,
		pack.ID, pack.CategoryID, strings.TrimSpace(pack.Name), strings.TrimSpace(pack.Description),
		pack.PriceCents, pack.Currency, pack.IsFree, pack.IsPurchasable, pack.IsActive, pack.SortOrder); err != nil {
		return nil, fmt.Errorf("upsert cosmetic pack: %w", mapCosmeticCatalogMutationError(err))
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_pack_items WHERE pack_id = $1`, pack.ID); err != nil {
		return nil, fmt.Errorf("replace cosmetic pack items: %w", err)
	}
	for index, itemID := range pack.ItemIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cosmetic_pack_items (pack_id, item_id, sort_order)
			VALUES ($1,$2,$3)`, pack.ID, itemID, (index+1)*10); err != nil {
			return nil, fmt.Errorf("insert cosmetic pack item: %w", mapCosmeticCatalogMutationError(err))
		}
	}
	after, _, err := cosmeticEntitySnapshot(ctx, tx, "pack", pack.ID)
	if err != nil {
		return nil, fmt.Errorf("read cosmetic pack after update: %w", err)
	}
	if err := writeCosmeticCatalogAudit(ctx, tx, catalogMutationAction(existed), "pack", pack.ID, actor, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("upsert cosmetic pack commit: %w", err)
	}
	pack.Name = strings.TrimSpace(pack.Name)
	pack.Description = strings.TrimSpace(pack.Description)
	pack.ItemIDs = append([]string(nil), pack.ItemIDs...)
	pack.Items = nil
	return &pack, nil
}

func DeleteCosmeticPack(ctx context.Context, packID, actor string) (bool, error) {
	return deleteCosmeticCatalogEntity(ctx, "pack", packID, actor)
}

func ListCosmeticCatalogAudit(ctx context.Context, limit int) ([]CosmeticCatalogAudit, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := Pool.Query(ctx, `
		SELECT id, actor, action, entity_type, entity_id, before_data, after_data, created_at
		FROM cosmetic_catalog_audit
		ORDER BY id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic catalog audit: %w", err)
	}
	defer rows.Close()
	audit := make([]CosmeticCatalogAudit, 0)
	for rows.Next() {
		var event CosmeticCatalogAudit
		if err := rows.Scan(&event.ID, &event.Actor, &event.Action, &event.EntityType, &event.EntityID,
			&event.Before, &event.After, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan cosmetic catalog audit: %w", err)
		}
		audit = append(audit, event)
	}
	return audit, rows.Err()
}

func deleteCosmeticCatalogEntity(ctx context.Context, entityType, entityID, actor string) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	if !cosmeticCatalogIDPattern.MatchString(entityID) {
		return false, ErrCosmeticCatalogInvalid
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("delete cosmetic %s begin: %w", entityType, err)
	}
	defer tx.Rollback(ctx)
	if err := lockCosmeticCatalogEntity(ctx, tx, entityType, entityID); err != nil {
		return false, err
	}
	before, exists, err := cosmeticEntitySnapshot(ctx, tx, entityType, entityID)
	if err != nil {
		return false, fmt.Errorf("read cosmetic %s before delete: %w", entityType, err)
	}
	if !exists {
		switch entityType {
		case "category":
			return false, ErrCosmeticCategoryNotFound
		case "item":
			return false, ErrCosmeticNotFound
		default:
			return false, ErrCosmeticPackNotFound
		}
	}
	var statement string
	var fallbackSlot string
	switch entityType {
	case "category":
		statement = `DELETE FROM cosmetic_categories WHERE id = $1`
	case "item":
		statement = `DELETE FROM cosmetic_items WHERE id = $1`
		if err := tx.QueryRow(ctx, `SELECT slot FROM cosmetic_items WHERE id = $1`, entityID).Scan(&fallbackSlot); err != nil {
			return false, fmt.Errorf("read deleted cosmetic item slot: %w", err)
		}
		if err := lockCosmeticFallbackSlot(ctx, tx, fallbackSlot); err != nil {
			return false, err
		}
	case "pack":
		statement = `DELETE FROM cosmetic_packs WHERE id = $1`
	default:
		return false, ErrCosmeticCatalogInvalid
	}
	result, err := tx.Exec(ctx, statement, entityID)
	if err != nil {
		return false, fmt.Errorf("delete cosmetic %s: %w", entityType, mapCosmeticCatalogMutationError(err))
	}
	if entityType == "item" {
		if err := requireCosmeticSlotFallback(ctx, tx, fallbackSlot); err != nil {
			return false, err
		}
	}
	if err := writeCosmeticCatalogAudit(ctx, tx, "delete", entityType, entityID, actor, before, nil); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("delete cosmetic %s commit: %w", entityType, err)
	}
	return result.RowsAffected() == 1, nil
}

func lockCosmeticCatalogEntity(ctx context.Context, tx pgx.Tx, entityType, entityID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "cosmetic-"+entityType+":"+entityID); err != nil {
		return fmt.Errorf("lock cosmetic %s: %w", entityType, err)
	}
	return nil
}

func lockCosmeticFallbackSlot(ctx context.Context, tx pgx.Tx, slot string) error {
	return lockCosmeticCatalogEntity(ctx, tx, "fallback", slot)
}

func lockAllCosmeticFallbackSlots(ctx context.Context, tx pgx.Tx) error {
	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment} {
		if err := lockCosmeticFallbackSlot(ctx, tx, slot); err != nil {
			return err
		}
	}
	return nil
}

func cosmeticEntitySnapshot(ctx context.Context, tx pgx.Tx, entityType, entityID string) (json.RawMessage, bool, error) {
	var query string
	switch entityType {
	case "category":
		query = `SELECT to_jsonb(category_row)::TEXT FROM cosmetic_categories category_row WHERE id = $1`
	case "item":
		query = `SELECT to_jsonb(item_row)::TEXT FROM cosmetic_items item_row WHERE id = $1`
	case "pack":
		query = `
			SELECT (to_jsonb(pack_row) || jsonb_build_object(
				'item_ids', COALESCE((
					SELECT jsonb_agg(item_id ORDER BY sort_order, item_id)
					FROM cosmetic_pack_items WHERE pack_id = pack_row.id
				), '[]'::jsonb)
			))::TEXT
			FROM cosmetic_packs pack_row WHERE id = $1`
	default:
		return nil, false, ErrCosmeticCatalogInvalid
	}
	var raw string
	if err := tx.QueryRow(ctx, query, entityID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return json.RawMessage(raw), true, nil
}

func writeCosmeticCatalogAudit(ctx context.Context, tx pgx.Tx, action, entityType, entityID, actor string, before, after json.RawMessage) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "admin-api"
	}
	if len(actor) > 160 {
		actor = actor[:160]
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_catalog_audit (actor, action, entity_type, entity_id, before_data, after_data)
		VALUES ($1,$2,$3,$4,$5,$6)`, actor, action, entityType, entityID, nullableCatalogJSON(before), nullableCatalogJSON(after)); err != nil {
		return fmt.Errorf("write cosmetic catalog audit: %w", err)
	}
	return nil
}

func nullableCatalogJSON(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func catalogMutationAction(existed bool) string {
	if existed {
		return "update"
	}
	return "create"
}

func requireCosmeticSlotFallback(ctx context.Context, tx pgx.Tx, slot string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cosmetic_items item
			JOIN cosmetic_categories category ON category.id = item.category_id
			WHERE item.slot = $1 AND item.is_active = true AND item.is_free = true
			  AND category.is_active = true
		)`, slot).Scan(&exists); err != nil {
		return fmt.Errorf("check cosmetic slot fallback: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: each cosmetic slot requires an active free fallback", ErrCosmeticCatalogConflict)
	}
	return nil
}

func requireAllCosmeticSlotFallbacks(ctx context.Context, tx pgx.Tx) error {
	for _, slot := range []string{CosmeticSlotBotSkin, CosmeticSlotWeaponSkin, CosmeticSlotAttachment} {
		if err := requireCosmeticSlotFallback(ctx, tx, slot); err != nil {
			return err
		}
	}
	return nil
}

func mapCosmeticCatalogMutationError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503", "23505":
		return fmt.Errorf("%w: %s", ErrCosmeticCatalogConflict, pgErr.ConstraintName)
	case "22001", "23514":
		return fmt.Errorf("%w: %s", ErrCosmeticCatalogInvalid, pgErr.ConstraintName)
	default:
		return err
	}
}
