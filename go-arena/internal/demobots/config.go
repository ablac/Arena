package demobots

// BotConfig defines the loadout and AI strategy for a single demo bot.
type BotConfig struct {
	Name     string
	Weapon   string
	Stats    map[string]int // hp, speed, attack, defense (must sum to 20, each 1-10)
	Strategy string         // aggressive, defensive, kite, territorial
	Color    string         // avatar hex color
}

// WeaponRanges maps weapon names to their effective range for AI decisions.
// Must match WeaponConfigs in game/weapons.go.
var WeaponRanges = map[string]float64{
	"sword":   2.5,
	"bow":     15.0,
	"daggers": 1.5,
	"shield":  1.8,
	"spear":   3.5,
	"staff":   12.0,
}

// DemoConfigs holds the 15 demo bot configurations, matching the Python
// demo_bot_ai.py DEMO_CONFIGS exactly.
var DemoConfigs = []BotConfig{
	{
		Name:     "Demo-Berserker",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 3, "speed": 4, "attack": 10, "defense": 3},
		Strategy: "aggressive",
		Color:    "#e94560",
	},
	{
		Name:     "Demo-Sniper",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 2, "speed": 8, "attack": 7, "defense": 3},
		Strategy: "kite",
		Color:    "#4ecdc4",
	},
	{
		Name:     "Demo-Tank",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 10, "speed": 2, "attack": 3, "defense": 5},
		Strategy: "territorial",
		Color:    "#556270",
	},
	{
		Name:     "Demo-Assassin",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 3, "speed": 9, "attack": 5, "defense": 3},
		Strategy: "aggressive",
		Color:    "#c44569",
	},
	{
		Name:     "Demo-Lancer",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 6, "defense": 4},
		Strategy: "aggressive",
		Color:    "#f5cd79",
	},
	{
		Name:     "Demo-Mage",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 3, "speed": 5, "attack": 7, "defense": 5},
		Strategy: "kite",
		Color:    "#6c5ce7",
	},
	{
		Name:     "Demo-Guardian",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 7, "speed": 3, "attack": 4, "defense": 6},
		Strategy: "defensive",
		Color:    "#3dc1d3",
	},
	{
		Name:     "Demo-Ranger",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 4, "speed": 7, "attack": 6, "defense": 3},
		Strategy: "kite",
		Color:    "#e77f67",
	},
	{
		Name:     "Demo-Brawler",
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 7, "defense": 3},
		Strategy: "aggressive",
		Color:    "#cf6a87",
	},
	{
		Name:     "Demo-Phantom",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 2, "speed": 10, "attack": 6, "defense": 2},
		Strategy: "kite",
		Color:    "#574b90",
	},
	{
		Name:     "Demo-Warden",
		Weapon:   "spear",
		Stats:    map[string]int{"hp": 6, "speed": 4, "attack": 5, "defense": 5},
		Strategy: "territorial",
		Color:    "#78e08f",
	},
	{
		Name:     "Demo-Warlock",
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 4, "speed": 4, "attack": 8, "defense": 4},
		Strategy: "defensive",
		Color:    "#e55039",
	},
	{
		Name:     "Demo-Sentinel",
		Weapon:   "shield",
		Stats:    map[string]int{"hp": 8, "speed": 3, "attack": 3, "defense": 6},
		Strategy: "defensive",
		Color:    "#60a3bc",
	},
	{
		Name:     "Demo-Duelist",
		Weapon:   "daggers",
		Stats:    map[string]int{"hp": 4, "speed": 7, "attack": 6, "defense": 3},
		Strategy: "aggressive",
		Color:    "#fa983a",
	},
	{
		Name:     "Demo-Marksman",
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 3, "speed": 6, "attack": 8, "defense": 3},
		Strategy: "kite",
		Color:    "#1e90ff",
	},
}
