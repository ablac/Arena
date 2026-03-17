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
}

// DemoConfigs — 15 demo bots with aggressive stat builds and combat-focused strategies.
//
// Design philosophy: all bots should fight. No passive idling. Shove constantly.
//   - Sword: high attack, berserker/aggressive
//   - Bow: attack-focused kite that prioritizes shooting over retreating
//   - Daggers: assassin that hunts and shoves
//   - Shield: territorial aggressor, shoves everything
//   - Spear: aggressive with knockback + shove combo
//   - Staff: aggressive kite, attacks > retreats
var DemoConfigs = []BotConfig{
	// === SWORD USERS (melee cleave, 0.5s cd) ===
	{
		Name:     "Demo-Berserker",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 4, "speed": 5, "attack": 9, "defense": 2},
		Strategy: "berserker",
		Color:    "#e94560",
	},
	{
		Name:     "Demo-Brawler",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 8, "defense": 2},
		Strategy: "aggressive",
		Color:    "#cf6a87",
	},

	// === BOW USERS (ranged projectile, 1.4s cd) ===
	{
		Name:     "Demo-Sniper",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 3, "speed": 5, "attack": 10, "defense": 2},
		Strategy: "kite",
		Color:    "#4ecdc4",
	},
	{
		Name:     "Demo-Ranger",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 4, "speed": 5, "attack": 8, "defense": 3},
		Strategy: "aggressive",
		Color:    "#e77f67",
	},
	{
		Name:     "Demo-Marksman",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 3, "speed": 6, "attack": 9, "defense": 2},
		Strategy: "kite",
		Color:    "#1e90ff",
	},

	// === DAGGER USERS (fast melee, double strike, 0.3s cd) ===
	{
		Name:     "Demo-Assassin",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 7, "attack": 8, "defense": 2},
		Strategy: "assassin",
		Color:    "#c44569",
	},
	{
		Name:     "Demo-Phantom",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 7, "attack": 8, "defense": 2},
		Strategy: "berserker",
		Color:    "#574b90",
	},
	{
		Name:     "Demo-Duelist",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 7, "defense": 3},
		Strategy: "aggressive",
		Color:    "#fa983a",
	},

	// === SHIELD USERS (tanky melee, block passive, 0.7s cd) ===
	{
		Name:     "Demo-Tank",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 7, "speed": 3, "attack": 5, "defense": 5},
		Strategy: "territorial",
		Color:    "#556270",
	},
	{
		Name:     "Demo-Guardian",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 6, "speed": 4, "attack": 6, "defense": 4},
		Strategy: "aggressive",
		Color:    "#3dc1d3",
	},
	{
		Name:     "Demo-Sentinel",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 7, "speed": 3, "attack": 5, "defense": 5},
		Strategy: "berserker",
		Color:    "#60a3bc",
	},

	// === SPEAR USERS (mid-range melee, knockback, 0.7s cd) ===
	{
		Name:     "Demo-Lancer",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 4, "speed": 5, "attack": 8, "defense": 3},
		Strategy: "aggressive",
		Color:    "#f5cd79",
	},
	{
		Name:     "Demo-Warden",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 5, "speed": 4, "attack": 7, "defense": 4},
		Strategy: "territorial",
		Color:    "#78e08f",
	},

	// === STAFF USERS (long range AoE, delayed, 1.3s cd) ===
	{
		Name:     "Demo-Mage",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 3, "speed": 5, "attack": 9, "defense": 3},
		Strategy: "kite",
		Color:    "#6c5ce7",
	},
	{
		Name:     "Demo-Warlock",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 5, "attack": 8, "defense": 3},
		Strategy: "aggressive",
		Color:    "#e55039",
	},
}
