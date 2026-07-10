package demobots

// BotConfig defines the loadout and AI strategy for a single demo bot.
type BotConfig struct {
	Name     string
	Weapon   string
	Stats    map[string]int // hp, speed, attack, defense (must sum to 20, each 1-10)
	Strategy string         // aggressive, defensive, kite, territorial, assassin, berserker
	Color    string         // avatar hex color
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
		Name:     "Reaper",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 5, "attack": 9, "defense": 2},
		Strategy: "kite",
		Color:    "#6c5ce7",
	},
	{
		Name:     "Archmage",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 6, "speed": 4, "attack": 8, "defense": 2},
		Strategy: "defensive",
		Color:    "#e55039",
	},

	// === SHIELD x2 ===
	{
		Name:     "Juggernaut",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 8, "speed": 4, "attack": 5, "defense": 3},
		Strategy: "territorial",
		Color:    "#556270",
	},
	{
		Name:     "Fortress",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 9, "speed": 3, "attack": 4, "defense": 4},
		Strategy: "defensive",
		Color:    "#3dc1d3",
	},

	// === SPEAR x2 ===
	{
		Name:     "Lancer",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 5, "speed": 6, "attack": 7, "defense": 2},
		Strategy: "aggressive",
		Color:    "#f5cd79",
	},
	{
		Name:     "Valkyrie",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 6, "speed": 7, "attack": 5, "defense": 2},
		Strategy: "territorial",
		Color:    "#78e08f",
	},

	// === GRAPPLE x2 ===
	{
		Name:     "Hook",
		Weapon:   "grapple",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 7, "defense": 3},
		Strategy: "assassin",
		Color:    "#c44569",
	},
	{
		Name:     "Scorpion",
		Weapon:   "grapple",
		Stats:    map[string]int{"hp": 6, "speed": 5, "attack": 6, "defense": 3},
		Strategy: "territorial",
		Color:    "#574b90",
	},

	// === DAGGERS x2 ===
	{
		Name:     "Shredder",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 4, "speed": 7, "attack": 7, "defense": 2},
		Strategy: "berserker",
		Color:    "#e94560",
	},
	{
		Name:     "Viper",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 5, "speed": 8, "attack": 5, "defense": 2},
		Strategy: "assassin",
		Color:    "#cf6a87",
	},

	// === BOW x2 ===
	{
		Name:     "Ranger",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 8, "defense": 2},
		Strategy: "kite",
		Color:    "#2ecc71",
	},
	{
		Name:     "Deadeye",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 5, "speed": 4, "attack": 9, "defense": 2},
		Strategy: "defensive",
		Color:    "#16a085",
	},

	// === SWORD x2 ===
	{
		Name:     "Warden",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 6, "speed": 5, "attack": 6, "defense": 3},
		Strategy: "defensive",
		Color:    "#3498db",
	},
	{
		Name:     "Executioner",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy: "berserker",
		Color:    "#c0392b",
	},
}
