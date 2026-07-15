package game

import (
	"context"
	"math"
	"sort"
	"strconv"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
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
			BotID:                  id,
			Weapon:                 weapon,
			Elo:                    1000,
			RoundKills:             kills,
			RoundWeaponKills:       kills,
			BestKillStreak:         kills,
			RoundDamageDealt:       float64(kills) * 80,
			RoundWeaponDamageDealt: float64(kills) * 80,
			RoundLongestLife:       600,
			RoundShotsFired:        40,
			RoundShotsHit:          20,
		}
	}
	assignBalanceEngagements(bots)
	return bots
}

func assignBalanceEngagements(bots map[string]*BotState) {
	for _, bot := range bots {
		bot.RoundWeaponOpponentIDs = make(map[string]struct{})
		for opponentID, opponent := range bots {
			if opponent != nil && opponentID != bot.BotID && opponent.Weapon != bot.Weapon {
				bot.RoundWeaponOpponentIDs[opponentID] = struct{}{}
			}
		}
	}
}

func runBalanceRounds(rounds int, bots map[string]*BotState, winnerID string) {
	for i := 0; i < rounds; i++ {
		AutoBalanceWeapons(context.Background(), bots, winnerID)
	}
}

func seedPositiveHeadlineWithInconclusiveAxes(t *testing.T, weapon string, rounds int) {
	t.Helper()
	entry := &weaponRoundPerformance{
		bots:            3,
		totalShotsFired: 120,
		totalShotsHit:   60,
		botIDs: map[string]struct{}{
			weapon + "-a": {},
			weapon + "-b": {},
			weapon + "-c": {},
		},
		engagedOpponentIDs: map[string]struct{}{
			"opponent-a": {},
			"opponent-b": {},
			"opponent-c": {},
		},
	}
	opponents := &weaponRoundPerformance{
		bots:            3,
		totalShotsFired: 120,
		totalShotsHit:   60,
	}
	evidence := &weaponBalanceEvidence{}
	for i := 0; i < rounds; i++ {
		axisPressure := 2.0
		if i%2 != 0 {
			axisPressure = -2.0
		}
		evidence.add(entry, opponents, 0.5, axisPressure, -axisPressure)
	}

	weaponBalanceMu.Lock()
	weaponEvidence[weapon] = evidence
	weaponBalanceMu.Unlock()
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
	if state.RoundsTracked != 3 || state.Revision != 3 {
		t.Fatalf("tracked rounds/revision = %d/%d, want 3/3", state.RoundsTracked, state.Revision)
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

func TestAutoBalanceAcceptsDistinctEngagedTwoBotCohort(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMinRounds = 6
	config.C.WeaponAutoBalanceMinBotSamples = 18
	config.C.WeaponAutoBalanceMinDistinctBots = 2
	bots := balanceTestBots(8, 0)
	delete(bots, "sword-c")
	delete(bots, "bow-f")
	assignBalanceEngagements(bots)

	runBalanceRounds(9, bots, "sword-a")

	strong, _ := GetWeaponBalanceState("sword")
	weak, _ := GetWeaponBalanceState("bow")
	if strong.DamageScale >= 1 || weak.DamageScale <= 1 {
		t.Fatalf("two-bot cohorts did not balance: sword=%.4f bow=%.4f", strong.DamageScale, weak.DamageScale)
	}
}

func TestAutoBalanceRejectsBystanderDiversityForSingleVictimFarm(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMinRounds = 6
	config.C.WeaponAutoBalanceMinBotSamples = 18
	config.C.WeaponAutoBalanceMinDistinctBots = 2

	// Keep three bow bots in the comparison roster, but attribute every sword
	// engagement to only bow-d. The other bots are bystanders and cannot satisfy
	// opponent diversity merely by being present in the arena.
	bots := balanceTestBots(8, 0)
	delete(bots, "sword-c")
	for _, bot := range bots {
		bot.RoundWeaponOpponentIDs = make(map[string]struct{})
		if bot.Weapon == "sword" {
			bot.RoundWeaponOpponentIDs["bow-d"] = struct{}{}
		} else {
			bot.RoundWeaponOpponentIDs["sword-a"] = struct{}{}
		}
	}
	runBalanceRounds(9, bots, "sword-a")

	for _, weapon := range []string{"sword", "bow"} {
		state, _ := GetWeaponBalanceState(weapon)
		if state.DamageScale != 1 || state.CooldownScale != 1 {
			t.Fatalf("single-opponent farming moved %s: damage=%.4f cooldown=%.4f", weapon, state.DamageScale, state.CooldownScale)
		}
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

func TestAutoBalanceRetainsInconclusiveEvidenceWithoutDecay(t *testing.T) {
	setupWeaponBalanceTest(t)
	initial, _ := GetWeaponBalanceState("sword")

	// The batch has a zero mean but a wide confidence interval. That is
	// inconclusive, not proof that the weapon is equivalent to its peers.
	for i := 0; i < 4; i++ {
		if i%2 == 0 {
			AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "sword-a")
		} else {
			AutoBalanceWeapons(context.Background(), balanceTestBots(0, 8), "bow-d")
		}
	}

	state, _ := GetWeaponBalanceState("sword")
	if state.AdjustmentScale != initial.AdjustmentScale {
		t.Fatalf("inconclusive evidence changed adjustment step: %.4f -> %.4f", initial.AdjustmentScale, state.AdjustmentScale)
	}
	weaponBalanceMu.RLock()
	retainedRounds := weaponEvidence["sword"].rounds
	weaponBalanceMu.RUnlock()
	if retainedRounds != 4 {
		t.Fatalf("inconclusive evidence retained %d rounds, want 4", retainedRounds)
	}
}

func TestAutoBalancePersistentNoisyAdvantageEventuallyAdjusts(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMaxEvidenceRounds = 48

	// Every four-round slice is too noisy for a 95% confidence decision, but
	// the long-running signal is consistently three strong rounds to one weak.
	for i := 0; i < 48; i++ {
		if i%4 == 3 {
			AutoBalanceWeapons(context.Background(), balanceTestBots(0, 8), "bow-d")
		} else {
			AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "sword-a")
		}
	}

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale >= 1 && state.CooldownScale <= 1 {
		t.Fatalf("persistent noisy advantage never adjusted: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
}

func TestAutoBalanceInconclusiveEvidenceExpiresWithoutDecay(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMaxEvidenceRounds = 8
	initial, _ := GetWeaponBalanceState("sword")

	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "sword-a")
		} else {
			AutoBalanceWeapons(context.Background(), balanceTestBots(0, 8), "bow-d")
		}
	}

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != initial.DamageScale || state.CooldownScale != initial.CooldownScale {
		t.Fatalf("expired headline noise moved weapon: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
	}
	if state.AdjustmentScale != initial.AdjustmentScale {
		t.Fatalf("expired inconclusive evidence changed adjustment step: %.4f -> %.4f", initial.AdjustmentScale, state.AdjustmentScale)
	}
	weaponBalanceMu.RLock()
	retainedRounds := weaponEvidence["sword"].rounds
	weaponBalanceMu.RUnlock()
	if retainedRounds != 0 {
		t.Fatalf("expired inconclusive evidence retained %d rounds, want 0", retainedRounds)
	}
}

func TestAutoBalanceMaxEvidenceCompositeNerfConvergesWhenAxesStayInconclusive(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMaxEvidenceRounds = 8
	minStep, _ := weaponBalanceStepBounds()
	initial, _ := GetWeaponBalanceState("sword")
	expectedDamage := 1.0
	expectedCooldown := 1.0

	for batch := 1; batch <= 3; batch++ {
		seedPositiveHeadlineWithInconclusiveAxes(t, "sword", config.C.WeaponAutoBalanceMaxEvidenceRounds-1)
		AutoBalanceWeapons(context.Background(), balanceTestBots(8, 0), "sword-a")

		expectedDamage *= 1 - minStep
		expectedCooldown *= 1 + minStep
		state, _ := GetWeaponBalanceState("sword")
		if math.Abs(state.DamageScale-expectedDamage) > 1e-12 {
			t.Fatalf("batch %d damage scale = %.12f, want bounded composite nerf %.12f", batch, state.DamageScale, expectedDamage)
		}
		if math.Abs(state.CooldownScale-expectedCooldown) > 1e-12 {
			t.Fatalf("batch %d cooldown scale = %.12f, want bounded composite nerf %.12f", batch, state.CooldownScale, expectedCooldown)
		}
		if state.AdjustmentScale != initial.AdjustmentScale {
			t.Fatalf("batch %d changed adjustment step: %.4f -> %.4f", batch, initial.AdjustmentScale, state.AdjustmentScale)
		}

		weaponBalanceMu.RLock()
		retainedRounds := weaponEvidence["sword"].rounds
		weaponBalanceMu.RUnlock()
		if retainedRounds != 0 {
			t.Fatalf("batch %d retained %d evidence rounds after resolution, want 0", batch, retainedRounds)
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
			bot.RoundWeaponKills = 0
			bot.RoundWeaponDamageDealt = 0
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
	bots := balanceTestBots(1, 1)
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

func TestAutoBalanceIgnoresNonWeaponRoundOutput(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(3, 3)
	for _, bot := range bots {
		if bot.Weapon == "sword" {
			// Simulate mines, objective rewards, and survival output that belongs
			// to the bot rather than the sword. Direct weapon counters stay even.
			bot.RoundKills = 99
			bot.RoundDamageDealt = 9999
			bot.BestKillStreak = 99
			bot.RoundLongestLife = 9000
		}
	}

	runBalanceRounds(4, bots, "sword-a")

	for _, weapon := range []string{"sword", "bow"} {
		state, _ := GetWeaponBalanceState(weapon)
		if state.DamageScale != 1 || state.CooldownScale != 1 {
			t.Fatalf("non-weapon output moved %s: damage=%.4f cooldown=%.4f", weapon, state.DamageScale, state.CooldownScale)
		}
	}
}

func TestManualWeaponChangeDiscardsPartialEvidence(t *testing.T) {
	setupWeaponBalanceTest(t)
	bots := balanceTestBots(8, 0)
	runBalanceRounds(3, bots, "sword-a")

	wc, ok := GetBaseWeaponConfig("sword")
	if !ok {
		t.Fatal("missing sword config")
	}
	original := wc
	t.Cleanup(func() { UpdateBaseWeaponConfig("sword", original) })
	wc.Damage++
	if !UpdateBaseWeaponConfig("sword", wc) {
		t.Fatal("manual sword update failed")
	}
	AutoBalanceWeapons(context.Background(), bots, "sword-a")

	state, _ := GetWeaponBalanceState("sword")
	if state.DamageScale != 1 || state.CooldownScale != 1 {
		t.Fatalf("pre-change evidence leaked: damage=%.4f cooldown=%.4f", state.DamageScale, state.CooldownScale)
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

func TestNormalizeWeaponBalanceStateClampsPersistedScales(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMinDamageScale = 0.70
	config.C.WeaponAutoBalanceMaxDamageScale = 1.40
	config.C.WeaponAutoBalanceMinCooldownScale = 0.75
	config.C.WeaponAutoBalanceMaxCooldownScale = 1.35

	weaponBalanceMu.Lock()
	weaponBalance["staff"] = WeaponBalanceState{
		Weapon:          "staff",
		DamageScale:     0.10,
		CooldownScale:   3.00,
		AdjustmentScale: 0.05,
	}
	weaponBalanceMu.Unlock()

	state, _ := GetWeaponBalanceState("staff")
	if state.DamageScale != 0.70 || state.CooldownScale != 1.35 {
		t.Fatalf("persisted scales normalized to %.2f/%.2f, want 0.70/1.35", state.DamageScale, state.CooldownScale)
	}
}

func TestWeaponBalanceStateFromRecordPreservesMigratedScalesAndClampsExtremes(t *testing.T) {
	setupWeaponBalanceTest(t)
	config.C.WeaponAutoBalanceMinDamageScale = 0.70
	config.C.WeaponAutoBalanceMaxDamageScale = 1.40
	config.C.WeaponAutoBalanceMinCooldownScale = 0.75
	config.C.WeaponAutoBalanceMaxCooldownScale = 1.35
	updatedAt := time.Now()

	staff := weaponBalanceStateFromRecord(db.WeaponBalance{
		Weapon: "staff", DamageScale: 0.97, CooldownScale: 1.19,
		AdjustmentScale: 0.04, RoundsTracked: 0, Revision: 9, UpdatedAt: updatedAt,
	})
	if staff.DamageScale != 0.97 || staff.CooldownScale != 1.19 || staff.AdjustmentScale != 0.04 ||
		staff.RoundsTracked != 0 || staff.Revision != 9 || !staff.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("loaded Staff = %+v, want preserved 0.97/1.19 scales and revision 9", staff)
	}

	extreme := weaponBalanceStateFromRecord(db.WeaponBalance{
		Weapon: "sword", DamageScale: 0.10, CooldownScale: 3.0,
		AdjustmentScale: 0.04,
	})
	if extreme.DamageScale != 0.70 || extreme.CooldownScale != 1.35 {
		t.Fatalf("loaded extreme state = %.2f/%.2f, want clamped 0.70/1.35", extreme.DamageScale, extreme.CooldownScale)
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

func TestRunningBalanceStatDistinguishesEquivalenceFromInconclusive(t *testing.T) {
	var equivalent runningBalanceStat
	for _, value := range []float64{0.01, -0.01, 0.01, -0.01} {
		equivalent.add(value)
	}
	if !equivalent.equivalentWithin(0.05, 1.96) {
		t.Fatal("tight interval inside deadzone was not classified equivalent")
	}

	var inconclusive runningBalanceStat
	for _, value := range []float64{0.4, -0.4, 0.4, -0.4} {
		inconclusive.add(value)
	}
	if inconclusive.equivalentWithin(0.05, 1.96) {
		t.Fatal("wide interval overlapping deadzone was classified equivalent")
	}
}

func TestBalanceAdjustmentMagnitudeHasFloor(t *testing.T) {
	if got := balanceAdjustmentMagnitude(0.005, 0.01, 0.005); got != 0.005 {
		t.Fatalf("adjustment magnitude = %.6f, want floor 0.005", got)
	}
}

func TestApplyRelativeScaleChangeCapsEachAxisAtTwoPercent(t *testing.T) {
	if got := applyRelativeScaleChange(1, 0.50, 0.005, 0.70, 1.40); math.Abs(got-1.02) > 1e-9 {
		t.Fatalf("positive capped scale = %.4f, want 1.02", got)
	}
	if got := applyRelativeScaleChange(1, -0.50, 0.005, 0.70, 1.40); math.Abs(got-0.98) > 1e-9 {
		t.Fatalf("negative capped scale = %.4f, want 0.98", got)
	}
	if got := applyRelativeScaleChange(1, 0.0001, 0.005, 0.70, 1.40); math.Abs(got-1.005) > 1e-9 {
		t.Fatalf("positive floored scale = %.4f, want 1.005", got)
	}
	if got := applyRelativeScaleChange(1, -0.0001, 0.005, 0.70, 1.40); math.Abs(got-0.995) > 1e-9 {
		t.Fatalf("negative floored scale = %.4f, want 0.995", got)
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
	addBotPerformance(entry, total, bot, bot.BotID, 0.75)

	if rate := entry.hitRate(); rate < 0 || rate > 1 {
		t.Fatalf("Elo-corrected hit rate escaped [0,1]: %.4f", rate)
	}
	if got, want := entry.totalShotsHit/entry.totalShotsFired, 2.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("shots fired/hit were not adjusted consistently: ratio=%.4f want %.4f", got, want)
	}
}

func BenchmarkAutoBalanceWeapons128Bots(b *testing.B) {
	previousConfig := config.C
	previousMode := ActiveModeRules
	b.Cleanup(func() {
		config.C = previousConfig
		ActiveModeRules = previousMode
	})

	config.C.WeaponAutoBalanceEnabled = true
	config.C.WeaponAutoBalanceStartStep = 0.05
	config.C.WeaponAutoBalanceMinStep = 0.005
	config.C.WeaponAutoBalanceDecay = 0.94
	config.C.WeaponAutoBalanceDeadzoneStart = 0.02
	config.C.WeaponAutoBalanceDeadzoneMin = 0.003
	config.C.WeaponAutoBalanceMinDamageScale = 0.70
	config.C.WeaponAutoBalanceMaxDamageScale = 1.40
	config.C.WeaponAutoBalanceMinCooldownScale = 0.75
	config.C.WeaponAutoBalanceMaxCooldownScale = 1.35
	config.C.WeaponAutoBalanceDamageWeight = 0.65
	config.C.WeaponAutoBalanceCooldownWeight = 0.45
	config.C.WeaponAutoBalanceMinRounds = 6
	config.C.WeaponAutoBalanceMinBotSamples = 18
	config.C.WeaponAutoBalanceMinDistinctBots = 2
	config.C.WeaponAutoBalanceMinActions = 5
	config.C.WeaponAutoBalanceConfidenceZ = 1.96
	config.C.WeaponAutoBalanceMinEffect = 0.05
	config.C.WeaponAutoBalanceMaxEvidenceRounds = 48
	ActiveModeRules = ModeRulesFor(ModeFFA)
	if err := LoadWeaponBalance(context.Background()); err != nil {
		b.Fatalf("LoadWeaponBalance: %v", err)
	}

	weaponBalanceMu.RLock()
	weapons := make([]string, 0, len(baseWeaponConfigs))
	for weapon := range baseWeaponConfigs {
		weapons = append(weapons, weapon)
	}
	weaponBalanceMu.RUnlock()
	sort.Strings(weapons)

	bots := make(map[string]*BotState, 128)
	for i := 0; i < 128; i++ {
		weapon := weapons[i%len(weapons)]
		id := weapon + "-benchmark-" + strconv.Itoa(i)
		kills := i % 6
		bots[id] = &BotState{
			BotID:                  id,
			Weapon:                 weapon,
			Elo:                    900 + i%201,
			RoundKills:             kills,
			RoundWeaponKills:       kills,
			BestKillStreak:         kills,
			RoundDamageDealt:       float64(120 + i%80),
			RoundWeaponDamageDealt: float64(120 + i%80),
			RoundLongestLife:       300 + i%120,
			RoundShotsFired:        20 + i%15,
			RoundShotsHit:          8 + i%12,
		}
	}
	assignBalanceEngagements(bots)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AutoBalanceWeapons(context.Background(), bots, "staff-benchmark-0")
	}
}
