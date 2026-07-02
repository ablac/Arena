package game

import (
	"context"
	"testing"

	"arena-server/internal/config"
)

func balanceTestBots(strongKills, weakKills int) map[string]*BotState {
	bots := map[string]*BotState{}
	for i, w := range []string{"sword", "sword", "bow", "bow"} {
		kills := weakKills
		if w == "sword" {
			kills = strongKills
		}
		id := string(rune('a' + i))
		bots[id] = &BotState{
			BotID:            id,
			Weapon:           w,
			RoundKills:       kills,
			BestKillStreak:   kills,
			RoundDamageDealt: float64(kills) * 80,
			RoundLongestLife: 600,
			RoundShotsFired:  40,
			RoundShotsHit:    20,
		}
	}
	return bots
}

func TestAutoBalanceSkipsTeamModes(t *testing.T) {
	loadTestConfig(t)
	config.C.WeaponAutoBalanceEnabled = true
	if err := LoadWeaponBalance(context.Background()); err != nil {
		t.Fatalf("LoadWeaponBalance: %v", err)
	}

	prev := ActiveModeRules
	defer func() { ActiveModeRules = prev }()
	ActiveModeRules = ModeRulesFor(ModeTeamBattle)

	AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1.0 || state.RoundsTracked != 0 {
		t.Errorf("team-mode round should not train the balancer, got scale=%.3f rounds=%d",
			state.DamageScale, state.RoundsTracked)
	}
}

func TestAutoBalanceCorrectsAndKeepsStepWhileUnbalanced(t *testing.T) {
	loadTestConfig(t)
	config.C.WeaponAutoBalanceEnabled = true
	if err := LoadWeaponBalance(context.Background()); err != nil {
		t.Fatalf("LoadWeaponBalance: %v", err)
	}
	prev := ActiveModeRules
	defer func() { ActiveModeRules = prev }()
	ActiveModeRules = ModeRulesFor(ModeFFA)

	initial, _ := GetWeaponBalanceState("sword")

	// Several rounds of a heavily overperforming sword.
	for i := 0; i < 5; i++ {
		AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "a")
	}

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale >= 1.0 {
		t.Errorf("overperforming weapon should be nerfed, damage scale = %.3f", state.DamageScale)
	}
	if state.RoundsTracked != 5 {
		t.Errorf("expected 5 tracked rounds, got %d", state.RoundsTracked)
	}
	// While still outside the deadzone the step must not decay below where
	// it started — the balancer keeps pushing instead of stalling.
	if state.AdjustmentScale < initial.AdjustmentScale {
		t.Errorf("adjustment step decayed while actively correcting: %.4f < %.4f",
			state.AdjustmentScale, initial.AdjustmentScale)
	}

	weakState, _ := GetWeaponBalanceState("bow")
	if weakState.DamageScale <= 1.0 {
		t.Errorf("underperforming weapon should be buffed, damage scale = %.3f", weakState.DamageScale)
	}
}
