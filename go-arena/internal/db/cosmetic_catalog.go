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
	ErrCosmeticCatalogBuiltin   = fmt.Errorf("%w: built-in catalog entries cannot be deleted; deactivate instead", ErrCosmeticCatalogConflict)
	ErrCosmeticCategoryNotFound = errors.New("cosmetic category not found")
	ErrCosmeticPackNotFound     = errors.New("cosmetic pack not found")
)

var cosmeticCatalogIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var cosmeticAssetKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var cosmeticCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var cosmeticRarityPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

const CosmeticPackPriceCents = 199

type CosmeticCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	IsBuiltin   bool   `json:"is_builtin"`
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
	IsBuiltin     bool           `json:"is_builtin"`
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

var launchCosmeticCategories = []CosmeticCategory{
	{ID: "chassis", Name: "Chassis", Description: "Presentation-only bot body finishes.", IsActive: true, SortOrder: 10},
	{ID: "weapon-finishes", Name: "Weapon Finishes", Description: "Presentation-only weapon materials.", IsActive: true, SortOrder: 20},
	{ID: "attachments", Name: "Attachments", Description: "Presentation-only bot accessories.", IsActive: true, SortOrder: 30},
	{ID: "starter-packs", Name: "Starter Packs", Description: "Curated cosmetic bundles using Arena's built-in procedural visuals.", IsActive: true, SortOrder: 40},
	{ID: "elemental-sets", Name: "Elemental Sets", Description: "Fire, ice, storm, earth, and ocean-inspired Arena sets.", IsActive: true, SortOrder: 50},
	{ID: "cosmic-sets", Name: "Cosmic Sets", Description: "Orbital, stellar, and deep-space Arena sets.", IsActive: true, SortOrder: 60},
	{ID: "cyber-sets", Name: "Cyber Sets", Description: "Neon, synthetic, and digital Arena sets.", IsActive: true, SortOrder: 70},
	{ID: "wild-sets", Name: "Wild Sets", Description: "Terrain and wildlife-inspired Arena sets.", IsActive: true, SortOrder: 80},
	{ID: "arcane-sets", Name: "Arcane Sets", Description: "Runic, mystical, and relic-inspired Arena sets.", IsActive: true, SortOrder: 90},
	{ID: "industrial-sets", Name: "Industrial Sets", Description: "Foundry, alloy, and machine-inspired Arena sets.", IsActive: true, SortOrder: 100},
	{ID: "royal-sets", Name: "Royal Sets", Description: "Regal heraldry and precious-metal Arena sets.", IsActive: true, SortOrder: 110},
	{ID: "abyssal-sets", Name: "Abyssal Sets", Description: "Void, shadow, and night-inspired Arena sets.", IsActive: true, SortOrder: 120},
	{ID: "apex-sets", Name: "Apex Sets", Description: "The rarest capstone Arena sets.", IsActive: true, SortOrder: 130},
}

var starterCosmeticCategories = buildStarterCosmeticCategories()

func buildStarterCosmeticCategories() []CosmeticCategory {
	categories := append([]CosmeticCategory(nil), launchCosmeticCategories...)
	for index := range categories {
		categories[index].IsBuiltin = true
	}
	return categories
}

var legacyStarterCosmeticPacks = []CosmeticPack{
	{
		ID: "neon-signal-pack", CategoryID: "starter-packs", Name: "Neon Signal Pack",
		Description: "Neon Grid chassis, Solar Flare weapon finish, and Signal Antenna.",
		PriceCents:  CosmeticPackPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 10,
		ItemIDs: []string{"skin-neon-grid", "weapon-solar-flare", "attachment-signal-antenna"},
	},
	{
		ID: "void-orbit-pack", CategoryID: "starter-packs", Name: "Void Orbit Pack",
		Description: "Carbon Armor chassis, Void Edge weapon finish, and Orbital Halo.",
		PriceCents:  CosmeticPackPriceCents, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: 20,
		ItemIDs: []string{"skin-carbon-armor", "weapon-void-edge", "attachment-orbital-halo"},
	},
}

type launchSetSeed struct {
	Name           string
	Slug           string
	CollectionID   string
	CollectionName string
}

var launchSetSeeds = []launchSetSeed{
	{Name: "Ember Vanguard", Slug: "ember_vanguard", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Glacier Circuit", Slug: "glacier_circuit", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Storm Herald", Slug: "storm_herald", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Terra Forge", Slug: "terra_forge", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Tidal Crown", Slug: "tidal_crown", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Solar Bloom", Slug: "solar_bloom", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Ashen Pulse", Slug: "ashen_pulse", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Frost Warden", Slug: "frost_warden", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Thunder Vale", Slug: "thunder_vale", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Magma Coil", Slug: "magma_coil", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Verdant Spark", Slug: "verdant_spark", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Tempest Alloy", Slug: "tempest_alloy", CollectionID: "elemental-sets", CollectionName: "Elemental Sets"},
	{Name: "Lunar Relay", Slug: "lunar_relay", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Nova Sentinel", Slug: "nova_sentinel", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Eclipse Runner", Slug: "eclipse_runner", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Comet Ward", Slug: "comet_ward", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Nebula Drift", Slug: "nebula_drift", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Pulsar Knight", Slug: "pulsar_knight", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Orbit Breaker", Slug: "orbit_breaker", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Starlight Vector", Slug: "starlight_vector", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Meteor Echo", Slug: "meteor_echo", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Quasar Guard", Slug: "quasar_guard", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Zenith Voyager", Slug: "zenith_voyager", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Astral Beacon", Slug: "astral_beacon", CollectionID: "cosmic-sets", CollectionName: "Cosmic Sets"},
	{Name: "Neon Cipher", Slug: "neon_cipher", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Chrome Phantom", Slug: "chrome_phantom", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Quantum Grid", Slug: "quantum_grid", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Vector Glitch", Slug: "vector_glitch", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Circuit Ronin", Slug: "circuit_ronin", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Pixel Reaver", Slug: "pixel_reaver", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Data Wraith", Slug: "data_wraith", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Synth Marshal", Slug: "synth_marshal", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Kernel Flux", Slug: "kernel_flux", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Prism Protocol", Slug: "prism_protocol", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Holo Striker", Slug: "holo_striker", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Binary Halo", Slug: "binary_halo", CollectionID: "cyber-sets", CollectionName: "Cyber Sets"},
	{Name: "Ironwood Fang", Slug: "ironwood_fang", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Coral Stalker", Slug: "coral_stalker", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Desert Viper", Slug: "desert_viper", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Alpine Rook", Slug: "alpine_rook", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Jungle Static", Slug: "jungle_static", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Arctic Lynx", Slug: "arctic_lynx", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Canyon Hawk", Slug: "canyon_hawk", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Moss Titan", Slug: "moss_titan", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Dune Jackal", Slug: "dune_jackal", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Reef Specter", Slug: "reef_specter", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Redwood Guard", Slug: "redwood_guard", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Savannah Bolt", Slug: "savannah_bolt", CollectionID: "wild-sets", CollectionName: "Wild Sets"},
	{Name: "Rune Keeper", Slug: "rune_keeper", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Mystic Anvil", Slug: "mystic_anvil", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Aether Shard", Slug: "aether_shard", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Hex Lantern", Slug: "hex_lantern", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Mana Crest", Slug: "mana_crest", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Oracle Veil", Slug: "oracle_veil", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Wisp Binder", Slug: "wisp_binder", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Sigil Knight", Slug: "sigil_knight", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Arcane Torrent", Slug: "arcane_torrent", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Spellforge Echo", Slug: "spellforge_echo", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Relic Pulse", Slug: "relic_pulse", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Enigma Ward", Slug: "enigma_ward", CollectionID: "arcane-sets", CollectionName: "Arcane Sets"},
	{Name: "Foundry Core", Slug: "foundry_core", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Brass Marauder", Slug: "brass_marauder", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Cobalt Rivet", Slug: "cobalt_rivet", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Diesel Crown", Slug: "diesel_crown", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Copper Bastion", Slug: "copper_bastion", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Steel Nomad", Slug: "steel_nomad", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Reactor Hound", Slug: "reactor_hound", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Piston Shade", Slug: "piston_shade", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Titanium Grit", Slug: "titanium_grit", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Gearstorm Unit", Slug: "gearstorm_unit", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Chrome Foundry", Slug: "chrome_foundry", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Alloy Rampart", Slug: "alloy_rampart", CollectionID: "industrial-sets", CollectionName: "Industrial Sets"},
	{Name: "Crimson Regent", Slug: "crimson_regent", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Sapphire Oath", Slug: "sapphire_oath", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Golden Banner", Slug: "golden_banner", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Ivory Marshal", Slug: "ivory_marshal", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Onyx Sovereign", Slug: "onyx_sovereign", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Emerald Order", Slug: "emerald_order", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Ruby Vanguard", Slug: "ruby_vanguard", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Silver Dynasty", Slug: "silver_dynasty", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Amethyst Crown", Slug: "amethyst_crown", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Royal Sentinel", Slug: "royal_sentinel", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Platinum Crest", Slug: "platinum_crest", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Obsidian Court", Slug: "obsidian_court", CollectionID: "royal-sets", CollectionName: "Royal Sets"},
	{Name: "Void Harrow", Slug: "void_harrow", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Umbral Fang", Slug: "umbral_fang", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Nightfall Engine", Slug: "nightfall_engine", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Abyss Walker", Slug: "abyss_walker", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Shadow Torrent", Slug: "shadow_torrent", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Darkstar Reign", Slug: "darkstar_reign", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Phantom Rift", Slug: "phantom_rift", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Blackout Crown", Slug: "blackout_crown", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Dread Signal", Slug: "dread_signal", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Null Specter", Slug: "null_specter", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Eventide Warden", Slug: "eventide_warden", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Midnight Ruin", Slug: "midnight_ruin", CollectionID: "abyssal-sets", CollectionName: "Abyssal Sets"},
	{Name: "Apex Radiance", Slug: "apex_radiance", CollectionID: "apex-sets", CollectionName: "Apex Sets"},
	{Name: "Omega Paragon", Slug: "omega_paragon", CollectionID: "apex-sets", CollectionName: "Apex Sets"},
}

var starterCosmeticPacks = buildStarterCosmeticPacks()

func launchSetEconomy(number int) (rarity string, itemPrice, packPrice int) {
	switch {
	case number <= 26:
		return "uncommon", 99, CosmeticPackPriceCents
	case number <= 50:
		return "rare", 149, CosmeticPackPriceCents
	case number <= 74:
		return "epic", 199, CosmeticPackPriceCents
	case number <= 94:
		return "legendary", 249, CosmeticPackPriceCents
	default:
		return "mythic", 349, CosmeticPackPriceCents
	}
}

func buildStarterCosmeticPacks() []CosmeticPack {
	packs := append([]CosmeticPack(nil), legacyStarterCosmeticPacks...)
	for index, seed := range launchSetSeeds {
		number := index + 3
		idSlug := strings.ReplaceAll(seed.Slug, "_", "-")
		_, _, packPrice := launchSetEconomy(number)
		packs = append(packs, CosmeticPack{
			ID: fmt.Sprintf("arena-set-%03d-%s-pack", number, idSlug), CategoryID: seed.CollectionID,
			Name: seed.Name + " Set", Description: "A coordinated three-piece " + seed.Name + " cosmetic set with no gameplay effects.",
			PriceCents: packPrice, Currency: "USD", IsPurchasable: true, IsActive: true, SortOrder: number * 10,
			ItemIDs: []string{
				fmt.Sprintf("skin-arena-set-%03d-%s", number, idSlug),
				fmt.Sprintf("weapon-arena-set-%03d-%s", number, idSlug),
				fmt.Sprintf("attachment-arena-set-%03d-%s", number, idSlug),
			},
		})
	}
	for index := range packs {
		packs[index].IsBuiltin = true
	}
	return packs
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
		!IsSupportedCosmeticAsset(item.Slot, item.AssetKey) ||
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
	if pack.IsPurchasable && !pack.IsFree && (pack.PriceCents != CosmeticPackPriceCents || pack.Currency != "USD") {
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
	// The launch storefront and Admin price fields use two-decimal minor units.
	// Keep the catalog USD-only until every display and refund path has a shared
	// ISO-4217 exponent table; accepting JPY/KRW here would charge a different
	// amount from the one Arena displays.
	if priceCents < 0 || priceCents > 1_000_000 || currency != "USD" {
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
		SELECT id, name, description, is_active, is_builtin, sort_order
		FROM cosmetic_categories `+categoryWhere+`
		ORDER BY sort_order, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list cosmetic categories: %w", err)
	}
	for categoryRows.Next() {
		var category CosmeticCategory
		if err := categoryRows.Scan(&category.ID, &category.Name, &category.Description, &category.IsActive, &category.IsBuiltin, &category.SortOrder); err != nil {
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
		       i.price_cents, i.currency, i.is_free, i.is_purchasable, i.is_active, i.is_builtin, i.sort_order
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
		       p.is_free, p.is_purchasable, p.is_active, p.is_builtin, p.sort_order
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
			&pack.PriceCents, &pack.Currency, &pack.IsFree, &pack.IsPurchasable, &pack.IsActive, &pack.IsBuiltin, &pack.SortOrder); err != nil {
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
		       i.price_cents, i.currency, i.is_free, i.is_purchasable, i.is_active, i.is_builtin, i.sort_order
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
			&item.IsPurchasable, &item.IsActive, &item.IsBuiltin, &item.SortOrder); err != nil {
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
		&item.Rarity, &item.PriceCents, &item.Currency, &item.IsFree, &item.IsPurchasable, &item.IsActive, &item.IsBuiltin, &item.SortOrder)
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
	if err := tx.QueryRow(ctx, `
		INSERT INTO cosmetic_categories (id, name, description, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, description = EXCLUDED.description,
			is_active = EXCLUDED.is_active, sort_order = EXCLUDED.sort_order, updated_at = NOW()
		RETURNING is_builtin`,
		category.ID, strings.TrimSpace(category.Name), strings.TrimSpace(category.Description), category.IsActive, category.SortOrder).Scan(&category.IsBuiltin); err != nil {
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
	if err := tx.QueryRow(ctx, `
		INSERT INTO cosmetic_items
			(id, name, description, category_id, slot, asset_key, rarity, price_cents, currency,
			 is_free, is_purchasable, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name, description = EXCLUDED.description, category_id = EXCLUDED.category_id,
			rarity = EXCLUDED.rarity, price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency,
			is_free = EXCLUDED.is_free, is_purchasable = EXCLUDED.is_purchasable,
			is_active = EXCLUDED.is_active, sort_order = EXCLUDED.sort_order, updated_at = NOW()
		RETURNING is_builtin`,
		item.ID, strings.TrimSpace(item.Name), strings.TrimSpace(item.Description), item.CategoryID, item.Slot,
		item.AssetKey, item.Rarity, item.PriceCents, item.Currency, item.IsFree, item.IsPurchasable, item.IsActive, item.SortOrder).Scan(&item.IsBuiltin); err != nil {
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
	if err := tx.QueryRow(ctx, `
		INSERT INTO cosmetic_packs
			(id, category_id, name, description, price_cents, currency, is_free, is_purchasable, is_active, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			category_id = EXCLUDED.category_id, name = EXCLUDED.name, description = EXCLUDED.description,
			price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency, is_free = EXCLUDED.is_free,
			is_purchasable = EXCLUDED.is_purchasable, is_active = EXCLUDED.is_active,
			sort_order = EXCLUDED.sort_order, updated_at = NOW()
		RETURNING is_builtin`,
		pack.ID, pack.CategoryID, strings.TrimSpace(pack.Name), strings.TrimSpace(pack.Description),
		pack.PriceCents, pack.Currency, pack.IsFree, pack.IsPurchasable, pack.IsActive, pack.SortOrder).Scan(&pack.IsBuiltin); err != nil {
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
	isBuiltin, err := cosmeticCatalogEntityIsBuiltin(ctx, tx, entityType, entityID)
	if err != nil {
		return false, fmt.Errorf("read cosmetic %s built-in state: %w", entityType, err)
	}
	if isBuiltin {
		return false, ErrCosmeticCatalogBuiltin
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

func cosmeticCatalogEntityIsBuiltin(ctx context.Context, tx pgx.Tx, entityType, entityID string) (bool, error) {
	var query string
	switch entityType {
	case "category":
		query = `SELECT is_builtin FROM cosmetic_categories WHERE id = $1`
	case "item":
		query = `SELECT is_builtin FROM cosmetic_items WHERE id = $1`
	case "pack":
		query = `SELECT is_builtin FROM cosmetic_packs WHERE id = $1`
	default:
		return false, ErrCosmeticCatalogInvalid
	}
	var isBuiltin bool
	if err := tx.QueryRow(ctx, query, entityID).Scan(&isBuiltin); err != nil {
		return false, err
	}
	return isBuiltin, nil
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
