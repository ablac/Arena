package game

import (
	"testing"
)

func TestGetWeaponConfig(t *testing.T) {
	weapons := []string{"sword", "bow", "daggers", "shield", "spear", "staff", "grapple"}
	for _, name := range weapons {
		wc := GetWeaponConfig(name)
		if wc.Name != name {
			t.Errorf("GetWeaponConfig(%q) returned name %q", name, wc.Name)
		}
		if wc.Damage <= 0 {
			t.Errorf("weapon %q has non-positive damage %d", name, wc.Damage)
		}
		if wc.GridRange <= 0 {
			t.Errorf("weapon %q has non-positive grid range %d", name, wc.GridRange)
		}
		if wc.Cooldown <= 0 {
			t.Errorf("weapon %q has non-positive cooldown %f", name, wc.Cooldown)
		}
	}
}

func TestGetWeaponConfigFallback(t *testing.T) {
	wc := GetWeaponConfig("unknown_weapon_xyz")
	if wc.Name != "sword" {
		t.Errorf("fallback should be sword, got %q", wc.Name)
	}
}

func TestGetAvailableWeapons(t *testing.T) {
	weapons := GetAvailableWeapons()
	if len(weapons) == 0 {
		t.Error("GetAvailableWeapons returned empty slice")
	}
	// Every weapon should have a config
	for _, w := range weapons {
		wc := GetWeaponConfig(w)
		if wc.Name != w {
			t.Errorf("weapon %q not found in config", w)
		}
	}
}

func TestCalculateDamage(t *testing.T) {
	tests := []struct {
		weaponDmg  float64
		attackMult float64
		defenseRed float64
		want       float64
	}{
		{25, 1.0, 0.0, 25},
		{25, 2.0, 0.0, 50},
		{25, 1.0, 0.5, 12.5},
		{25, 1.0, 1.0, 0},
		{25, 1.0, -0.1, 25},   // negative defense clamped to 0
		{25, 1.0, 1.5, 0},     // defense > 1 clamped to 1
		{0, 2.0, 0.0, 0},
	}
	for _, tc := range tests {
		got := CalculateDamage(tc.weaponDmg, tc.attackMult, tc.defenseRed)
		if got != tc.want {
			t.Errorf("CalculateDamage(%v, %v, %v) = %v, want %v",
				tc.weaponDmg, tc.attackMult, tc.defenseRed, got, tc.want)
		}
	}
}

func TestIsWeaponReady(t *testing.T) {
	if !IsWeaponReady(0) {
		t.Error("cooldown=0 should be ready")
	}
	if !IsWeaponReady(-1) {
		t.Error("cooldown=-1 should be ready")
	}
	if IsWeaponReady(0.1) {
		t.Error("cooldown=0.1 should not be ready")
	}
	if IsWeaponReady(1.0) {
		t.Error("cooldown=1.0 should not be ready")
	}
}

func TestWeaponSpecials(t *testing.T) {
	specials := map[string]string{
		"sword":   "cleave",
		"bow":     "projectile",
		"daggers": "double_strike",
		"shield":  "block",
		"spear":   "knockback",
		"staff":   "area",
		"grapple": "grapple",
	}
	for weapon, special := range specials {
		wc := GetWeaponConfig(weapon)
		if wc.Special != special {
			t.Errorf("weapon %q: expected special=%q, got %q", weapon, special, wc.Special)
		}
	}
}
