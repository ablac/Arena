package game

import "arena-server/internal/config"

// WeaponConfig defines the properties of a weapon type.
type WeaponConfig struct {
	Name     string
	Damage   int
	Range    float64
	Cooldown float64 // seconds
	Special  string  // "cleave", "projectile", "double_strike", "block", "knockback", "area"
	Param    float64 // special-specific parameter
}

// WeaponConfigs maps weapon name to its configuration.
var WeaponConfigs map[string]WeaponConfig

func init() {
	WeaponConfigs = map[string]WeaponConfig{
		"sword": {
			Name:     "sword",
			Damage:   25,
			Range:    2.5,
			Cooldown: 0.5,
			Special:  "cleave",
		},
		"bow": {
			Name:     "bow",
			Damage:   12,
			Range:    15.0,
			Cooldown: 1.4,
			Special:  "projectile",
		},
		"daggers": {
			Name:     "daggers",
			Damage:   12,
			Range:    1.5,
			Cooldown: 0.3,
			Special:  "double_strike",
			Param:    0.25,
		},
		"shield": {
			Name:     "shield",
			Damage:   15,
			Range:    1.8,
			Cooldown: 0.7,
			Special:  "block",
			Param:    0.5,
		},
		"spear": {
			Name:     "spear",
			Damage:   20,
			Range:    3.5,
			Cooldown: 0.7,
			Special:  "knockback",
			Param:    2.0,
		},
		"staff": {
			Name:     "staff",
			Damage:   18,
			Range:    12.0,
			Cooldown: 1.3,
			Special:  "area",
			Param:    3.0,
		},
	}
}

// GetWeaponConfig returns the configuration for the named weapon.
// Falls back to sword if the name is not recognized.
func GetWeaponConfig(name string) WeaponConfig {
	if wc, ok := WeaponConfigs[name]; ok {
		return wc
	}
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
	return weaponDmg * attackMult * (1.0 - defenseRed)
}

// IsInRange returns true if the edge-to-edge distance between attacker and
// target is at most weaponRange. BotRadius is subtracted from each side so
// that melee weapons can connect despite bot-separation enforcement.
func IsInRange(attacker, target Vec2, weaponRange float64) bool {
	dist := attacker.DistanceTo(target) - 2*config.C.BotRadius
	if dist < 0 {
		dist = 0
	}
	return dist <= weaponRange
}

// IsWeaponReady returns true if the weapon cooldown has expired.
func IsWeaponReady(cooldownRemaining float64) bool {
	return cooldownRemaining <= 0
}
