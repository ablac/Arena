package demobots

// BotConfig defines the loadout and AI strategy for a single demo bot.
type BotConfig struct {
	Name     string
	Weapon   string
	Stats    map[string]int // hp, speed, attack, defense (must sum to 20, each 1-10)
	Strategy string         // aggressive, defensive, kite, territorial, assassin, berserker
	Color    string         // avatar hex color
}

// WeaponRanges maps weapon names to their GridRange (Chebyshev tiles).
// These are fallbacks — the bot uses attack_range from loadout_confirmed when available.
var WeaponRanges = map[string]float64{
	"sword":   1,
	"bow":     7,
	"daggers": 1,
	"shield":  1,
	"spear":   2,
	"staff":   5,
	"grapple": 4,
}

// DemoConfigs — 15 demo bots built around 5 dominant archetypes (×3 each).
//
// Design philosophy: exploit weapon math. Staff AoE, Shield block, Spear knockback,
// Grapple anti-kite, Daggers burst. No swords, no bows — they lose.
var DemoConfigs = []BotConfig{
	// === REAPERS ×3 (Staff) — AoE destruction machines ===
	// Staff: 18 × (1+10×0.1) / 1.3s = 27.7 DPS per target, AoE hits multiple
	{
		Name:     "Demo-Reaper",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 4, "attack": 10, "defense": 2},
		Strategy: "kite",
		Color:    "#6c5ce7",
	},
	{
		Name:     "Demo-Archmage",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 4, "attack": 10, "defense": 2},
		Strategy: "kite",
		Color:    "#e55039",
	},
	{
		Name:     "Demo-Hellfire",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 4, "attack": 10, "defense": 2},
		Strategy: "kite",
		Color:    "#ff6348",
	},

	// === JUGGERNAUTS ×3 (Shield) — Unkillable zone holders ===
	// Shield: 15 × (1+6×0.1) / 0.7s = 34.3 DPS + 50% passive block + 180 HP
	{
		Name:     "Demo-Juggernaut",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 8, "speed": 3, "attack": 6, "defense": 3},
		Strategy: "territorial",
		Color:    "#556270",
	},
	{
		Name:     "Demo-Fortress",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 8, "speed": 3, "attack": 6, "defense": 3},
		Strategy: "territorial",
		Color:    "#3dc1d3",
	},
	{
		Name:     "Demo-Bulwark",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 8, "speed": 3, "attack": 6, "defense": 3},
		Strategy: "territorial",
		Color:    "#60a3bc",
	},

	// === LANCERS ×3 (Spear) — Knockback combo specialists ===
	// Spear: 20 × (1+8×0.1) / 0.7s = 51.4 DPS + knockback (wall splat = +5dmg)
	{
		Name:     "Demo-Lancer",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy: "aggressive",
		Color:    "#f5cd79",
	},
	{
		Name:     "Demo-Valkyrie",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy: "aggressive",
		Color:    "#78e08f",
	},
	{
		Name:     "Demo-Dragoon",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy: "aggressive",
		Color:    "#e77f67",
	},

	// === HOOKS ×3 (Grapple) — Anti-kite assassins ===
	// Grapple: 15 × (1+8×0.1) / 0.8s = 33.75 DPS + pulls you TO target
	{
		Name:     "Demo-Hook",
		Weapon:   "grapple",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#c44569",
	},
	{
		Name:     "Demo-Scorpion",
		Weapon:   "grapple",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#574b90",
	},
	{
		Name:     "Demo-Harpoon",
		Weapon:   "grapple",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#fa983a",
	},

	// === SHREDDERS ×3 (Daggers) — Burst DPS hunters ===
	// Daggers: 12 × (1+8×0.1) × 1.25 / 0.3s = 90 DPS (double strike)
	{
		Name:     "Demo-Shredder",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 7, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#e94560",
	},
	{
		Name:     "Demo-Viper",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 7, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#cf6a87",
	},
	{
		Name:     "Demo-Blitz",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 7, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#fc5c65",
	},
}
