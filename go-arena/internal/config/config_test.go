package config

import "testing"

func TestResolveShoveSettingsUsesWholePositiveGridTiles(t *testing.T) {
	tests := []struct {
		name                     string
		rangeIn, knockbackIn     float64
		rangeWant, knockbackWant float64
	}{
		{name: "defaults remain exact", rangeIn: 1, knockbackIn: 2, rangeWant: 1, knockbackWant: 2},
		{name: "fractional overrides round once", rangeIn: 1.6, knockbackIn: 3.4, rangeWant: 2, knockbackWant: 3},
		{name: "nonpositive values use defaults", rangeIn: 0, knockbackIn: -2, rangeWant: DefaultShoveRangeTiles, knockbackWant: DefaultShoveKnockbackTiles},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeGot, knockbackGot := resolveShoveSettings(tt.rangeIn, tt.knockbackIn)
			if rangeGot != tt.rangeWant || knockbackGot != tt.knockbackWant {
				t.Fatalf("resolveShoveSettings(%v, %v) = (%v, %v), want (%v, %v)", tt.rangeIn, tt.knockbackIn, rangeGot, knockbackGot, tt.rangeWant, tt.knockbackWant)
			}
		})
	}
}

func TestResolveEloSettings(t *testing.T) {
	tests := []struct {
		name                           string
		min, max, starting             int
		wantMin, wantMax, wantStarting int
	}{
		{name: "valid custom values", min: 800, max: 1600, starting: 1200, wantMin: 800, wantMax: 1600, wantStarting: 1200},
		{name: "inverted bounds use one default pair", min: 5000, max: 2000, starting: 1500, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1500},
		{name: "nonpositive bounds use one default pair", min: 0, max: 0, starting: 1000, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1000},
		{name: "high starting rating clamps", min: 800, max: 1200, starting: 5000, wantMin: 800, wantMax: 1200, wantStarting: 1200},
		{name: "low starting rating clamps", min: 800, max: 1200, starting: 200, wantMin: 800, wantMax: 1200, wantStarting: 800},
		{name: "missing starting rating uses bounded default", min: 1500, max: 2000, starting: 0, wantMin: 1500, wantMax: 2000, wantStarting: 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minElo, maxElo, startingElo := resolveEloSettings(tt.min, tt.max, tt.starting)
			if minElo != tt.wantMin || maxElo != tt.wantMax || startingElo != tt.wantStarting {
				t.Fatalf("resolveEloSettings(%d, %d, %d) = (%d, %d, %d), want (%d, %d, %d)",
					tt.min, tt.max, tt.starting, minElo, maxElo, startingElo,
					tt.wantMin, tt.wantMax, tt.wantStarting)
			}
		})
	}
}

func TestEloHelpersUseSameDefensiveBounds(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	C.EloMin = 5000
	C.EloMax = 2000
	C.EloStarting = 9000

	minElo, maxElo := EloBounds()
	if minElo != DefaultEloMin || maxElo != DefaultEloMax {
		t.Fatalf("EloBounds() = %d..%d, want %d..%d", minElo, maxElo, DefaultEloMin, DefaultEloMax)
	}
	if got := StartingElo(); got != DefaultEloMax {
		t.Fatalf("StartingElo() = %d, want %d", got, DefaultEloMax)
	}
	if got := ClampElo(-1); got != DefaultEloMin {
		t.Fatalf("ClampElo(-1) = %d, want %d", got, DefaultEloMin)
	}
	if got := ClampElo(99999); got != DefaultEloMax {
		t.Fatalf("ClampElo(99999) = %d, want %d", got, DefaultEloMax)
	}
}

func TestResolveWeaponAutoBalanceSettings(t *testing.T) {
	tests := []struct {
		name                             string
		minDamage, maxDamage             float64
		minCooldown, maxCooldown         float64
		maxEvidenceRounds                int
		wantMinDamage, wantMaxDamage     float64
		wantMinCooldown, wantMaxCooldown float64
		wantMaxEvidenceRounds            int
	}{
		{
			name: "valid widened rails", minDamage: 0.65, maxDamage: 1.50,
			minCooldown: 0.70, maxCooldown: 1.45, maxEvidenceRounds: 72,
			wantMinDamage: 0.65, wantMaxDamage: 1.50,
			wantMinCooldown: 0.70, wantMaxCooldown: 1.45, wantMaxEvidenceRounds: 72,
		},
		{
			name: "inverted damage rail falls back", minDamage: 1.20, maxDamage: 0.80,
			minCooldown: 0.75, maxCooldown: 1.35, maxEvidenceRounds: 48,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: 0.75, wantMaxCooldown: 1.35, wantMaxEvidenceRounds: 48,
		},
		{
			name: "rails must contain neutral", minDamage: 1.05, maxDamage: 1.50,
			minCooldown: 0.20, maxCooldown: 0.90, maxEvidenceRounds: 1,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: DefaultWeaponAutoBalanceMinCooldownScale, wantMaxCooldown: DefaultWeaponAutoBalanceMaxCooldownScale,
			wantMaxEvidenceRounds: DefaultWeaponAutoBalanceMaxEvidenceRounds,
		},
		{
			name: "absolute safety rails reject extreme values", minDamage: 0.01, maxDamage: 9,
			minCooldown: 0.01, maxCooldown: 9, maxEvidenceRounds: 9999,
			wantMinDamage: DefaultWeaponAutoBalanceMinDamageScale, wantMaxDamage: DefaultWeaponAutoBalanceMaxDamageScale,
			wantMinCooldown: DefaultWeaponAutoBalanceMinCooldownScale, wantMaxCooldown: DefaultWeaponAutoBalanceMaxCooldownScale,
			wantMaxEvidenceRounds: DefaultWeaponAutoBalanceMaxEvidenceRounds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minDamage, maxDamage, minCooldown, maxCooldown, maxEvidenceRounds := resolveWeaponAutoBalanceSettings(
				tt.minDamage, tt.maxDamage, tt.minCooldown, tt.maxCooldown, tt.maxEvidenceRounds,
			)
			if minDamage != tt.wantMinDamage || maxDamage != tt.wantMaxDamage ||
				minCooldown != tt.wantMinCooldown || maxCooldown != tt.wantMaxCooldown ||
				maxEvidenceRounds != tt.wantMaxEvidenceRounds {
				t.Fatalf("resolved balance settings = %.2f..%.2f / %.2f..%.2f / %d, want %.2f..%.2f / %.2f..%.2f / %d",
					minDamage, maxDamage, minCooldown, maxCooldown, maxEvidenceRounds,
					tt.wantMinDamage, tt.wantMaxDamage, tt.wantMinCooldown, tt.wantMaxCooldown, tt.wantMaxEvidenceRounds)
			}
		})
	}
}

func TestWeaponAutoBalanceHelpersUseDefensiveDefaults(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	C.WeaponAutoBalanceMinDamageScale = 2
	C.WeaponAutoBalanceMaxDamageScale = 3
	C.WeaponAutoBalanceMinCooldownScale = -1
	C.WeaponAutoBalanceMaxCooldownScale = 4
	C.WeaponAutoBalanceMaxEvidenceRounds = 0

	minDamage, maxDamage := WeaponAutoBalanceDamageBounds()
	minCooldown, maxCooldown := WeaponAutoBalanceCooldownBounds()
	if minDamage != DefaultWeaponAutoBalanceMinDamageScale || maxDamage != DefaultWeaponAutoBalanceMaxDamageScale {
		t.Fatalf("damage bounds = %.2f..%.2f", minDamage, maxDamage)
	}
	if minCooldown != DefaultWeaponAutoBalanceMinCooldownScale || maxCooldown != DefaultWeaponAutoBalanceMaxCooldownScale {
		t.Fatalf("cooldown bounds = %.2f..%.2f", minCooldown, maxCooldown)
	}
	if got := WeaponAutoBalanceEvidenceLimit(6); got != DefaultWeaponAutoBalanceMaxEvidenceRounds {
		t.Fatalf("evidence limit = %d, want %d", got, DefaultWeaponAutoBalanceMaxEvidenceRounds)
	}
}

func TestWeaponAutoBalanceStepBounds(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })

	C.WeaponAutoBalanceMinStep = 0.004
	C.WeaponAutoBalanceStartStep = 0.04
	if minStep, startStep := WeaponAutoBalanceStepBounds(); minStep != 0.004 || startStep != 0.04 {
		t.Fatalf("valid step bounds = %.3f/%.3f, want 0.004/0.040", minStep, startStep)
	}

	C.WeaponAutoBalanceMinStep = -1
	C.WeaponAutoBalanceStartStep = 9
	if minStep, startStep := WeaponAutoBalanceStepBounds(); minStep != 0.005 || startStep != 0.05 {
		t.Fatalf("defensive step bounds = %.3f/%.3f, want 0.005/0.050", minStep, startStep)
	}
}
