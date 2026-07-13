package demobots

import (
	"fmt"

	"arena-server/internal/db"
)

// BotConfig defines the loadout and AI strategy for a single demo bot.
type BotConfig struct {
	Name            string
	Weapon          string
	Stats           map[string]int // hp, speed, attack, defense (must sum to 20, each 1-10)
	Strategy        string         // aggressive, defensive, kite, territorial, assassin, berserker
	Color           string         // avatar hex color
	CosmeticPackID  string         // complete catalog pack exercised by this built-in bot
	CosmeticTrailID string         // separate one-item trail product exercised by this built-in bot
}

func cosmeticSelectionForTrail(catalog db.CosmeticCatalog, cosmeticID string) (cosmeticSelection, error) {
	if cosmeticID == "" {
		return cosmeticSelection{}, nil
	}
	for _, item := range catalog.Items {
		if item.ID != cosmeticID {
			continue
		}
		if item.Slot != db.CosmeticSlotTrail || item.CategoryID != db.CosmeticTrailCategoryID ||
			item.AssetKey == "standard" || !item.IsActive {
			return cosmeticSelection{}, fmt.Errorf("cosmetic trail %q is not an active paid trail", cosmeticID)
		}
		return cosmeticSelection{Slot: db.CosmeticSlotTrail, CosmeticID: item.ID}, nil
	}
	return cosmeticSelection{}, fmt.Errorf("cosmetic trail %q was not found", cosmeticID)
}

type cosmeticSelection struct {
	Slot       string
	CosmeticID string
}

func cosmeticSelectionsForPack(catalog db.CosmeticCatalog, packID string) ([]cosmeticSelection, error) {
	if packID == "" {
		return nil, nil
	}
	for _, pack := range catalog.Packs {
		if pack.ID != packID {
			continue
		}
		bySlot := make(map[string]string, len(pack.Items))
		for _, item := range pack.Items {
			if !db.IsValidCosmeticSlot(item.Slot) || bySlot[item.Slot] != "" {
				return nil, fmt.Errorf("cosmetic pack %q does not contain one item per slot", packID)
			}
			bySlot[item.Slot] = item.ID
		}
		selections := make([]cosmeticSelection, 0, 3)
		for _, slot := range []string{db.CosmeticSlotBotSkin, db.CosmeticSlotWeaponSkin, db.CosmeticSlotAttachment} {
			cosmeticID := bySlot[slot]
			if cosmeticID == "" {
				return nil, fmt.Errorf("cosmetic pack %q is missing slot %q", packID, slot)
			}
			selections = append(selections, cosmeticSelection{Slot: slot, CosmeticID: cosmeticID})
		}
		if len(pack.Items) != len(selections) {
			return nil, fmt.Errorf("cosmetic pack %q contains unsupported extra items", packID)
		}
		return selections, nil
	}
	return nil, fmt.Errorf("cosmetic pack %q was not found", packID)
}

// WeaponRanges maps weapon names to their GridRange (Chebyshev tiles),
// mirroring game/weapon_balance.go baseWeaponConfigs.
// These are fallbacks — the bot uses attack_range from loadout_confirmed when available.
var WeaponRanges = map[string]float64{
	"sword":   1,
	"bow":     8,
	"daggers": 1,
	"shield":  1,
	"spear":   2,
	"staff":   6,
	"grapple": 5,
}

// DemoConfigs defines 14 built-in arena bots, with two bots per weapon so the
// automatic balance system gets live samples across the full weapon roster.
var DemoConfigs = []BotConfig{
	// === STAFF x2 ===
	{
		Name:            "Reaper",
		Weapon:          "staff",
		Stats:           map[string]int{"hp": 4, "speed": 5, "attack": 9, "defense": 2},
		Strategy:        "kite",
		Color:           "#6c5ce7",
		CosmeticPackID:  "neon-signal-pack",
		CosmeticTrailID: "trail-ember-sparks",
	},
	{
		Name:            "Archmage",
		Weapon:          "staff",
		Stats:           map[string]int{"hp": 6, "speed": 4, "attack": 8, "defense": 2},
		Strategy:        "defensive",
		Color:           "#e55039",
		CosmeticPackID:  "arena-set-003-ember-vanguard-pack",
		CosmeticTrailID: "trail-frost-shards",
	},

	// === SHIELD x2 ===
	{
		Name:            "Juggernaut",
		Weapon:          "shield",
		Stats:           map[string]int{"hp": 8, "speed": 4, "attack": 5, "defense": 3},
		Strategy:        "territorial",
		Color:           "#556270",
		CosmeticPackID:  "void-orbit-pack",
		CosmeticTrailID: "trail-ion-stream",
	},
	{
		Name:            "Fortress",
		Weapon:          "shield",
		Stats:           map[string]int{"hp": 9, "speed": 3, "attack": 4, "defense": 4},
		Strategy:        "defensive",
		Color:           "#3dc1d3",
		CosmeticPackID:  "arena-set-004-glacier-circuit-pack",
		CosmeticTrailID: "trail-plasma-ribbon",
	},

	// === SPEAR x2 ===
	{
		Name:            "Lancer",
		Weapon:          "spear",
		Stats:           map[string]int{"hp": 5, "speed": 6, "attack": 7, "defense": 2},
		Strategy:        "aggressive",
		Color:           "#f5cd79",
		CosmeticPackID:  "arena-set-015-lunar-relay-pack",
		CosmeticTrailID: "trail-void-motes",
	},
	{
		Name:            "Valkyrie",
		Weapon:          "spear",
		Stats:           map[string]int{"hp": 6, "speed": 7, "attack": 5, "defense": 2},
		Strategy:        "territorial",
		Color:           "#78e08f",
		CosmeticPackID:  "arena-set-087-void-harrow-pack",
		CosmeticTrailID: "trail-solar-wake",
	},

	// === GRAPPLE x2 ===
	{
		Name:            "Hook",
		Weapon:          "grapple",
		Stats:           map[string]int{"hp": 4, "speed": 6, "attack": 7, "defense": 3},
		Strategy:        "assassin",
		Color:           "#c44569",
		CosmeticPackID:  "arena-set-005-storm-herald-pack",
		CosmeticTrailID: "trail-lunar-dust",
	},
	{
		Name:            "Scorpion",
		Weapon:          "grapple",
		Stats:           map[string]int{"hp": 6, "speed": 5, "attack": 6, "defense": 3},
		Strategy:        "territorial",
		Color:           "#574b90",
		CosmeticPackID:  "arena-set-008-solar-bloom-pack",
		CosmeticTrailID: "trail-comet-tail",
	},

	// === DAGGERS x2 ===
	{
		Name:            "Shredder",
		Weapon:          "daggers",
		Stats:           map[string]int{"hp": 4, "speed": 7, "attack": 7, "defense": 2},
		Strategy:        "berserker",
		Color:           "#e94560",
		CosmeticPackID:  "arena-set-006-terra-forge-pack",
		CosmeticTrailID: "trail-nebula-pulse",
	},
	{
		Name:            "Viper",
		Weapon:          "daggers",
		Stats:           map[string]int{"hp": 5, "speed": 8, "attack": 5, "defense": 2},
		Strategy:        "assassin",
		Color:           "#cf6a87",
		CosmeticPackID:  "arena-set-021-orbit-breaker-pack",
		CosmeticTrailID: "trail-storm-arcs",
	},

	// === BOW x2 ===
	{
		Name:            "Ranger",
		Weapon:          "bow",
		Stats:           map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
		Strategy:        "kite",
		Color:           "#2ecc71",
		CosmeticPackID:  "arena-set-030-vector-glitch-pack",
		CosmeticTrailID: "trail-static-glitch",
	},
	{
		Name:            "Deadeye",
		Weapon:          "bow",
		Stats:           map[string]int{"hp": 5, "speed": 4, "attack": 9, "defense": 2},
		Strategy:        "defensive",
		Color:           "#16a085",
		CosmeticPackID:  "arena-set-031-circuit-ronin-pack",
		CosmeticTrailID: "trail-pixel-scatter",
	},

	// === SWORD x2 ===
	{
		Name:            "Warden",
		Weapon:          "sword",
		Stats:           map[string]int{"hp": 6, "speed": 5, "attack": 6, "defense": 3},
		Strategy:        "defensive",
		Color:           "#3498db",
		CosmeticPackID:  "arena-set-038-binary-halo-pack",
		CosmeticTrailID: "trail-data-stream",
	},
	{
		Name:            "Executioner",
		Weapon:          "sword",
		Stats:           map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy:        "berserker",
		Color:           "#c0392b",
		CosmeticPackID:  "arena-set-100-omega-paragon-pack",
		CosmeticTrailID: "trail-holo-prism",
	},
}
