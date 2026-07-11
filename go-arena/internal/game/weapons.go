package game

import "arena-server/internal/config"

// WeaponConfig defines the properties of a weapon type.
type WeaponConfig struct {
	Name        string
	Damage      int     // rounded display/protocol damage
	DamageExact float64 // exact adaptive damage used by combat
	DamageScale float64 // adaptive multiplier used by derived weapon effects
	Range       float64 // float range (computed from GridRange * CellSize)
	GridRange   int     // range in grid tiles (Chebyshev distance)
	Cooldown    float64 // seconds
	Special     string  // "cleave", "projectile", "backstab", "bash", "knockback", "area"
	Param       float64 // special-specific parameter
	GridParam   int     // special param in grid tiles (e.g. staff area radius)
}

// WeaponConfigs maps weapon name to its effective runtime configuration.
var WeaponConfigs map[string]WeaponConfig

func weaponDamage(wc *WeaponConfig) float64 {
	if wc == nil {
		return 0
	}
	if finitePositive(wc.DamageExact) {
		return wc.DamageExact
	}
	return float64(wc.Damage)
}

func weaponDamageScale(wc *WeaponConfig) float64 {
	if wc != nil && finitePositive(wc.DamageScale) {
		return wc.DamageScale
	}
	return 1
}

// GetAvailableWeapons returns the list of all weapon names.
func GetAvailableWeapons() []string {
	return []string{"sword", "bow", "daggers", "shield", "spear", "staff", "grapple"}
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
