package game

import (
	"context"
	"math"
	"testing"

	"arena-server/internal/config"
)

func setupWeaponBalanceTest(t *testing.T) {
	t.Helper()
	loadTestConfig(t)
	config.C.WeaponAutoBalanceEnabled = true
	config.C.WeaponAutoBalanceMinRounds = 4
	config.C.WeaponAutoBalanceMinBotSamples = 12
	config.C.WeaponAutoBalanceMinDistinctBots = 3
	config.C.WeaponAutoBalanceMinActions = 5
	config.C.WeaponAutoBalanceConfidenceZ = 1.96
	config.C.WeaponAutoBalanceMinEffect = 0.05
	if err := LoadWeaponBalance(context.Background()); err != nil {
		t.Fatalf("LoadWeaponBalance: %v", err)
	}

	previousMode := ActiveModeRules
	ActiveModeRules = ModeRulesFor(ModeFFA)
	t.Cleanup(func() { ActiveModeRules = previousMode })
}

func balanceTestBots(strongKills, weakKills int) map[string]*BotState {
	bots := make(map[string]*BotState)
	for i, weapon := range []string{"sword", "sword", "sword", "bow", "bow", "bow"} {
		kills := weakKills
		if weapon == "sword" {
			kills = strongKills
		}
		id := weapon + "-" + string(rune('a'+i))
		bots[id] = &BotState{
			BotID:            id,
			Weapon:           weapon,
			Elo:              1000,
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

func runBalanceRounds(rounds int, bots map[string]*BotState, winnerID string) {
	for i := 0; i < rounds; i++ {
		AutoBalanceWeapons(context.Background(), bots, winnerID)
	}
}

func TestAutoBalanceSkipsTeamModes(t *testing.T) {
	setupWeaponBalanceTest(t)
	ActiveModeRules = ModeRulesFor(ModeTeamBattle)

	runBalanceRounds(10, balanceTestBots(8, 0), "sword-a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.RoundsTracked != 0 {
		t.Fatalf("team rounds trained balancer: scale=%.3f rounds=%d", state.DamageScale, state.RoundsTracked)
	}
}

func TestAutoBalanceWaitsForEvidenceBatch(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(8, 0)

	runBalanceRounds(3, bots, "sword-a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.CooldownScale != 1 {
		t.Fatalf("partial evidence batch changed weapon: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
	if state.RoundsTracked != 3 {
		t.Fatalf("tracked rounds = %d, want 3", state.RoundsTracked)
	}
}

func TestAutoBalanceCorrectsConsistentEvidence(t *testing.T) {
	setupWeaponBalanceTest(t)
	initial, _ := GetWeaponBalanceState("sword")

	runBalanceRounds(4, balanceTestBots(8, 0), "sword-a")

	strong, _ := GetWeaponBalanceState("sword")
	if strong.DamageScale >= 1 {
		t.Errorf("consistent overperformance did not nerf damage: %.4f", strong.DamageScale)
	}
	if strong.AdjustmentScale < initial.AdjustmentScale {
		t.Errorf("active correction decayed step: %.4f < %.4f", strong.AdjustmentScale, initial.AdjustmentScale)
	}
	weak, _ := GetWeaponBalanceState("bow")
	if weak.DamageScale <= 1 {
		t.Errorf("consistent underperformance did not buff damage: %.4f", weak.DamageScale)
	}
}

func TestAutoBalanceRejectsAlternatingNoise(t *testing.T) {
	setupWeaponBalanceTest(t)

	for i := 0; i < 4; i++ {
		if i%2 == 0 {
			AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "sword-a")
		} else {
			AutoBalanceWeapons(context.Background(), balanceTestBots(0, 8), "bow-d")
		}
	}

	for _, weapon := range []string{"sword", "bow"} {
		state, _ := GetWeaponBalanceState(weapon)
		if state.DamageScale != 1 || state.CooldownScale != 1 {
			t.Errorf("alternating noise moved %s: damage=%.4f cooldown=%.4f", weapon, state.DamageScale, state.CooldownScale)
		}
	}
}

func TestAutoBalanceIgnoresInactiveAndUnknownSamples(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(4, 4)
	for _, bot := range bots {
		if bot.Weapon == "sword" {
			bot.RoundShotsFired = 0
			bot.RoundShotsHit = 0
			bot.RoundKills = 0
			bot.RoundDamageDealt = 0
		}
	}
	bots["unknown"] = &BotState{
		BotID: "unknown", Weapon: "laser", Elo: 1000,
		RoundKills: 99, RoundDamageDealt: 9999,
		RoundShotsFired: 100, RoundShotsHit: 100,
	}

	runBalanceRounds(8, bots, "unknown")

	for _, weapon := range []string{"sword", "bow"} {
		state, _ := GetWeaponBalanceState(weapon)
		if state.RoundsTracked != 0 || state.DamageScale != 1 {
			t.Errorf("invalid sample trained %s: rounds=%d scale=%.4f", weapon, state.RoundsTracked, state.DamageScale)
		}
	}
	if _, ok := GetWeaponBalanceState("laser"); ok {
		t.Error("unknown weapon gained balance state")
	}
}

func TestAutoBalanceDoesNotRewardDeliberateMisses(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(0, 5)
	for _, bot := range bots {
		if bot.Weapon == "sword" {
			bot.RoundShotsHit = 0
		}
	}

	runBalanceRounds(4, bots, "bow-d")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.CooldownScale != 1 {
		t.Fatalf("throwing earned a buff: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
}

func TestAutoBalanceRequiresAxisEvidence(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(0, 1)
	for _, bot := range bots {
		if bot.Weapon == "sword" {
			bot.RoundLongestLife = 4000
		} else {
			bot.RoundLongestLife = 100
		}
	}

	runBalanceRounds(4, bots, "sword-a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.CooldownScale != 1 {
		t.Fatalf("survival confounder was attributed to weapon axes: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
}

func TestAutoBalanceSteadyBatchConvergesStep(t *testing.T) {
	setupWeaponBalanceTest(t)
	initial, _ := GetWeaponBalanceState("sword")

	runBalanceRounds(4, balanceTestBots(3, 3), "")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.CooldownScale != 1 {
		t.Fatalf("balanced batch changed scales: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
	if state.AdjustmentScale >= initial.AdjustmentScale {
		t.Fatalf("steady evidence did not decay step: %.4f >= %.4f", state.AdjustmentScale, initial.AdjustmentScale)
	}
}

func TestAutoBalanceLoadDiscardsPartialEvidence(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(8, 0)
	runBalanceRounds(3, bots, "sword-a")

	if err := LoadWeaponBalance(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	AutoBalanceWeapons(context.Background(), bots, "sword-a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.RoundsTracked != 1 {
		t.Fatalf("pre-restart evidence leaked: scale=%.4f rounds=%d", state.DamageScale, state.RoundsTracked)
	}
}

func TestRunningBalanceStatRequiresConfidenceInterval(t *testing.T) {
	var noisy runningBalanceStat
	for _, value := range []float64{0.4, -0.4, 0.4, -0.4} {
		noisy.add(value)
	}
	if got := noisy.directionOutside(0.05, 1.96); got != 0 {
		t.Fatalf("noisy evidence direction = %d, want 0", got)
	}

	var consistent runningBalanceStat
	for i := 0; i < 4; i++ {
		consistent.add(0.2)
	}
	if got := consistent.directionOutside(0.05, 1.96); got != 1 {
		t.Fatalf("consistent evidence direction = %d, want 1", got)
	}
}

func TestEloSkillFactorIsBounded(t *testing.T) {
	high := eloSkillFactor(1800, 1000)
	low := eloSkillFactor(200, 1000)
	if math.Abs(high-1.25) > 1e-9 || math.Abs(low-0.75) > 1e-9 {
		t.Fatalf("unexpected Elo bounds: high=%.3f low=%.3f", high, low)
	}
	if eloSkillFactor(0, 1000) != 1 {
		t.Error("missing Elo should be neutral")
	}
}

func TestEloCorrectionPreservesHitRateInvariant(t *testing.T) {
	entry := &weaponRoundPerformance{}
	total := &weaponRoundPerformance{}
	bot := &BotState{
		BotID: "low-elo", Weapon: "staff", RoundShotsFired: 10,
		// Area attacks can record more targets hit than casts fired.
		RoundShotsHit: 20, RoundLongestLife: 100,
	}
	addBotPerformance(entry, total, bot, bot.BotID, false, 0.75)

	if rate := entry.hitRate(); rate < 0 || rate > 1 {
		t.Fatalf("Elo-corrected hit rate escaped [0,1]: %.4f", rate)
	}
	if got, want := entry.totalShotsHit/entry.totalShotsFired, 2.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("shots fired/hit were not adjusted consistently: ratio=%.4f want %.4f", got, want)
	}
}
