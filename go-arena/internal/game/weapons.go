package game

import (
	"log/slog"

	"arena-server/internal/config"
)

// WeaponConfig defines the properties of a weapon type.
type WeaponConfig struct {
	Name      string
	Damage    int
	Range     float64 // float range (computed from GridRange * CellSize)
	GridRange int     // range in grid tiles (Chebyshev distance)
	Cooldown  float64 // seconds
	Special   string  // "cleave", "projectile", "double_strike", "block", "knockback", "area"
	Param     float64 // special-specific parameter
	GridParam int     // special param in grid tiles (e.g. staff area radius)
}

// WeaponConfigs maps weapon name to its configuration.
var WeaponConfigs map[string]WeaponConfig

func init() {
	WeaponConfigs = map[string]WeaponConfig{
		"sword": {
			Name:      "sword",
			Damage:    25,
			GridRange: 1,
			Cooldown:  0.5,
			Special:   "cleave",
		},
		"bow": {
			Name:      "bow",
			Damage:    12,
			GridRange: 7,
			Cooldown:  1.4,
			Special:   "projectile",
		},
		"daggers": {
			Name:      "daggers",
			Damage:    12,
			GridRange: 1,
			Cooldown:  0.3,
			Special:   "double_strike",
			Param:     0.25,
		},
		"shield": {
			Name:      "shield",
			Damage:    15,
			GridRange: 1,
			Cooldown:  0.7,
			Special:   "block",
			Param:     0.5,
		},
		"spear": {
			Name:      "spear",
			Damage:    20,
			GridRange: 2,
			Cooldown:  0.7,
			Special:   "knockback",
			Param:     2.0,
		},
		"staff": {
			Name:      "staff",
			Damage:    18,
			GridRange: 5,
			Cooldown:  1.3,
			Special:   "area",
			GridParam: 2,
		},
	}
}

// InitWeaponRanges computes the float Range from GridRange * CellSize.
// Must be called after config.Load().
func InitWeaponRanges(cellSize float64) {
	for name, wc := range WeaponConfigs {
		wc.Range = float64(wc.GridRange) * cellSize
		WeaponConfigs[name] = wc
	}
}

// GetWeaponConfig returns the configuration for the named weapon.
// Falls back to sword if the name is not recognized.
func GetWeaponConfig(name string) WeaponConfig {
	if wc, ok := WeaponConfigs[name]; ok {
		return wc
	}
	slog.Warn("unknown weapon, falling back to sword", "weapon", name)
	return WeaponConfigs["sword"]
}

// GetAvailableWeapons returns the list of all weapon names.
func GetAvailableWeapons() []string {
	return []string{"sword", "bow", "daggers", "shield", "spear", "staff"}
}

// CalculateDamage computes effective damage from a weapon hit.
//
//	damage = weaponDmg * attackMult * (1 - defenseRed)
//
// Shield passive reduction is handled separately in the damage subsystem.
func CalculateDamage(weaponDmg, attackMult, defenseRed float64) float64 {
	if defenseRed < 0 {
		defenseRed = 0
	} else if defenseRed > 1 {
		defenseRed = 1
	}
	return weaponDmg * attackMult * (1.0 - defenseRed)
}

// IsInRange returns true if the Chebyshev grid distance between attacker and
// target is at most gridRange tiles. Falls back to float distance if no
// terrain grid is active.
func IsInRange(attacker, target Vec2, gridRange int) bool {
	if ActiveTerrain != nil {
		aCell := ActiveTerrain.WorldToGrid(attacker)
		tCell := ActiveTerrain.WorldToGrid(target)
		return GridDistance(aCell, tCell) <= gridRange
	}
	// Fallback: float distance.
	dist := attacker.DistanceTo(target) - 2*config.C.BotRadius
	if dist < 0 {
		dist = 0
	}
	return dist <= float64(gridRange)*config.C.PathfindingCellSize
}

// IsWeaponReady returns true if the weapon cooldown has expired.
func IsWeaponReady(cooldownRemaining float64) bool {
	return cooldownRemaining <= 0
}
