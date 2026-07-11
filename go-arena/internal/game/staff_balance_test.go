package game

import (
	"context"
	"math"
	"testing"

	"arena-server/internal/config"
)

func setupStaffBalanceCombatTest(t *testing.T) {
	t.Helper()

	previousConfig := config.C
	config.Load()
	previousMode := ActiveModeRules
	previousTerrain := ActiveTerrain
	previousSuddenDeath := ActiveSuddenDeath
	previousEventHook := GameEventHook

	weaponBalanceMu.Lock()
	previousBalance := weaponBalance
	previousEvidence := weaponEvidence
	previousWeaponConfigs := WeaponConfigs
	weaponBalance = make(map[string]WeaponBalanceState, len(baseWeaponConfigs))
	weaponEvidence = make(map[string]*weaponBalanceEvidence, len(baseWeaponConfigs))
	WeaponConfigs = cloneWeaponConfigs(baseWeaponConfigs)
	for weapon := range baseWeaponConfigs {
		weaponBalance[weapon] = defaultWeaponBalanceState(weapon)
	}
	refreshAllWeaponConfigsLocked()
	weaponBalanceMu.Unlock()

	config.C.TickRate = 10
	config.C.PathfindingCellSize = 20
	config.C.ArenaWidth = 1000
	config.C.ArenaHeight = 1000
	config.C.StaffDelayTicks = 1
	config.C.StaffBurnFieldTicks = 4
	config.C.StaffBurnFieldRadius = 1
	config.C.StaffBurnFieldTickInterval = 1
	config.C.StaffBurnFieldDamage = 10
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
	config.C.WeaponAutoBalanceMinRounds = 4
	config.C.WeaponAutoBalanceMinBotSamples = 12
	config.C.WeaponAutoBalanceMinDistinctBots = 3
	config.C.WeaponAutoBalanceMinActions = 5
	config.C.WeaponAutoBalanceConfidenceZ = 1.96
	config.C.WeaponAutoBalanceMinEffect = 0.05
	config.C.WeaponAutoBalanceMaxEvidenceRounds = 48
	ActiveModeRules = ModeRulesFor(ModeFFA)
	ActiveTerrain = nil
	ActiveSuddenDeath = nil
	GameEventHook = nil

	t.Cleanup(func() {
		weaponBalanceMu.Lock()
		weaponBalance = previousBalance
		weaponEvidence = previousEvidence
		WeaponConfigs = previousWeaponConfigs
		weaponBalanceMu.Unlock()
		config.C = previousConfig
		ActiveModeRules = previousMode
		ActiveTerrain = previousTerrain
		ActiveSuddenDeath = previousSuddenDeath
		GameEventHook = previousEventHook
	})
}

func setTestWeaponDamageScale(weapon string, scale float64) {
	weaponBalanceMu.Lock()
	defer weaponBalanceMu.Unlock()
	state := defaultWeaponBalanceState(weapon)
	state.DamageScale = scale
	weaponBalance[weapon] = state
	WeaponConfigs[weapon] = effectiveWeaponConfigLocked(weapon)
}

func requireClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %.12f, want %.12f", got, want)
	}
}

func TestAdaptiveDamageRemainsFractionalOnCombatPath(t *testing.T) {
	setupStaffBalanceCombatTest(t)
	setTestWeaponDamageScale("sword", 0.99)

	wc := GetWeaponConfig("sword")
	if wc.Damage != 21 {
		t.Fatalf("rounded display damage = %d, want 21", wc.Damage)
	}
	requireClose(t, wc.DamageExact, 20.79)

	attacker := &BotState{
		BotID: "sword-attacker", Weapon: "sword", IsAlive: true, HP: 100,
		AttackMultiplier: 1, Position: NewVec2(300, 300),
	}
	target := &BotState{
		BotID: "target", Weapon: "bow", IsAlive: true, HP: 100,
		Position: NewVec2(310, 300),
	}
	bots := map[string]*BotState{attacker.BotID: attacker, target.BotID: target}
	grid := NewSpatialGrid(20)
	grid.Insert(attacker.BotID, attacker.Position)
	grid.Insert(target.BotID, target.Position)

	processMeleeAttack(attacker, target, &wc, bots, nil, nil, grid, 10)

	requireClose(t, attacker.RoundWeaponDamageDealt, 20.79)
	requireClose(t, target.HP, 79.21)
	if attacker.RoundShotsFired != 1 || attacker.RoundShotsHit != 1 {
		t.Fatalf("sword cast telemetry = %d/%d, want 1/1", attacker.RoundShotsFired, attacker.RoundShotsHit)
	}
}

func TestStaffBurnUsesAdaptiveScaleDefenseAndOneHitPerCast(t *testing.T) {
	setupStaffBalanceCombatTest(t)
	setTestWeaponDamageScale("staff", 0.83)

	attacker := &BotState{
		BotID: "staff-attacker", Weapon: "staff", IsAlive: true, HP: 100,
		AttackMultiplier: 1.5, Position: NewVec2(100, 100),
	}
	target := &BotState{
		BotID: "target", Weapon: "sword", IsAlive: true, HP: 100,
		DefenseReduction: 0.20, Position: NewVec2(700, 700),
	}
	targetPos := NewVec2(120, 100)
	action := &Action{Type: ActionAttack, TargetPosition: &targetPos}
	wc := GetWeaponConfig("staff")
	var impacts []StaffImpact

	processStaffAttack(attacker, nil, action, &wc, nil, &impacts, 10)

	if len(impacts) != 1 {
		t.Fatalf("staff impacts = %d, want 1", len(impacts))
	}
	requireClose(t, wc.DamageExact, 14.11)
	requireClose(t, impacts[0].Damage, 14.11)
	requireClose(t, impacts[0].DamageScale, 0.83)
	if attacker.RoundShotsFired != 1 || attacker.RoundShotsHit != 0 {
		t.Fatalf("pre-impact telemetry = %d/%d, want 1/0", attacker.RoundShotsFired, attacker.RoundShotsHit)
	}

	bots := map[string]*BotState{attacker.BotID: attacker, target.BotID: target}
	var fields []BurnField
	ProcessStaffImpacts(&impacts, &fields, bots, nil, 11)
	if len(fields) != 1 {
		t.Fatalf("burn fields = %d, want 1", len(fields))
	}
	requireClose(t, fields[0].Damage, 8.3)
	requireClose(t, fields[0].AttackMult, 1.5)

	target.Position = targetPos
	expectedPulse := 10.0 * 0.83 * 1.5 * (1 - target.DefenseReduction)
	ProcessBurnFields(&fields, bots, nil, 12)
	requireClose(t, target.HP, 100-expectedPulse)
	requireClose(t, attacker.RoundWeaponDamageDealt, expectedPulse)
	if attacker.RoundShotsHit != 1 || !fields[0].HitRecorded {
		t.Fatalf("first burn telemetry = hits %d recorded %v, want 1/true", attacker.RoundShotsHit, fields[0].HitRecorded)
	}

	ProcessBurnFields(&fields, bots, nil, 13)
	requireClose(t, target.HP, 100-2*expectedPulse)
	requireClose(t, attacker.RoundWeaponDamageDealt, 2*expectedPulse)
	if attacker.RoundShotsHit != 1 {
		t.Fatalf("repeated burn pulses counted as independent hits: %d", attacker.RoundShotsHit)
	}
}

func TestStaffDirectAOECountsOneHitPerCast(t *testing.T) {
	setupStaffBalanceCombatTest(t)
	config.C.StaffBurnFieldTicks = 0

	position := NewVec2(300, 300)
	attacker := &BotState{
		BotID: "staff-attacker", Weapon: "staff", IsAlive: true, HP: 100,
		AttackMultiplier: 1, Position: NewVec2(200, 300),
	}
	bots := map[string]*BotState{attacker.BotID: attacker}
	for i := 0; i < 3; i++ {
		id := string(rune('a' + i))
		bots[id] = &BotState{BotID: id, Weapon: "bow", IsAlive: true, HP: 100, Position: position}
	}
	action := &Action{Type: ActionAttack, TargetPosition: &position}
	wc := GetWeaponConfig("staff")
	var impacts []StaffImpact
	processStaffAttack(attacker, nil, action, &wc, nil, &impacts, 20)
	var fields []BurnField
	ProcessStaffImpacts(&impacts, &fields, bots, nil, 21)

	if attacker.RoundShotsFired != 1 || attacker.RoundShotsHit != 1 {
		t.Fatalf("staff AOE telemetry = %d/%d, want one hit for one cast", attacker.RoundShotsFired, attacker.RoundShotsHit)
	}
	requireClose(t, attacker.RoundWeaponDamageDealt, 3*wc.DamageExact)
	if len(attacker.RoundWeaponOpponentIDs) != 3 {
		t.Fatalf("staff engagements = %d, want 3 affected opponents", len(attacker.RoundWeaponOpponentIDs))
	}
}

func TestStaffBurnPreservesFriendlyFireGuard(t *testing.T) {
	setupStaffBalanceCombatTest(t)
	ActiveModeRules = ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}

	position := NewVec2(300, 300)
	attacker := &BotState{BotID: "staff", Weapon: "staff", Team: 1, IsAlive: true, RoundShotsFired: 1}
	ally := &BotState{BotID: "ally", Weapon: "sword", Team: 1, IsAlive: true, HP: 100, Position: position}
	enemy := &BotState{BotID: "enemy", Weapon: "sword", Team: 2, IsAlive: true, HP: 100, Position: position}
	fields := []BurnField{{
		OwnerID: attacker.BotID, Position: position, Radius: 1, Damage: 10,
		AttackMult: 1, TicksLeft: 3, TickInterval: 1,
	}}
	bots := map[string]*BotState{attacker.BotID: attacker, ally.BotID: ally, enemy.BotID: enemy}

	ProcessBurnFields(&fields, bots, nil, 30)

	if ally.HP != 100 {
		t.Fatalf("friendly Staff burn dealt damage: hp=%v", ally.HP)
	}
	if enemy.HP != 90 {
		t.Fatalf("enemy Staff burn hp=%v, want 90", enemy.HP)
	}
	if attacker.RoundShotsHit != 1 {
		t.Fatalf("friendly-fire-filtered cast hits = %d, want 1", attacker.RoundShotsHit)
	}
}

func TestStaffCombatTelemetryDrivesScopedBalanceNerf(t *testing.T) {
	setupStaffBalanceCombatTest(t)
	config.C.StaffBurnFieldTicks = 0
	beforeStaff := GetWeaponConfig("staff")

	bots := make(map[string]*BotState, 6)
	staffBots := make([]*BotState, 3)
	shieldBots := make([]*BotState, 3)
	for i := 0; i < 3; i++ {
		staff := &BotState{
			BotID: string(rune('a' + i)), Weapon: "staff", IsAlive: true,
			HP: 1e9, MaxHP: 1e9, Elo: 1000, AttackMultiplier: 1,
		}
		shield := &BotState{
			BotID: string(rune('x' + i)), Weapon: "shield", IsAlive: true,
			HP: 1e9, MaxHP: 1e9, Elo: 1000, AttackMultiplier: 1,
		}
		staffBots[i] = staff
		shieldBots[i] = shield
		bots[staff.BotID] = staff
		bots[shield.BotID] = shield
	}

	impactPosition := NewVec2(300, 300)
	for round := 0; round < 4; round++ {
		for _, bot := range bots {
			bot.ResetRoundStats()
			bot.IsAlive = true
			bot.HP = 1e9
			bot.RoundLongestLife = 600
		}
		for i, staff := range staffBots {
			staff.Position = NewVec2(200, 260+float64(i*40))
		}
		for _, shield := range shieldBots {
			shield.Position = impactPosition
		}

		staffWC := GetWeaponConfig("staff")
		var impacts []StaffImpact
		for _, staff := range staffBots {
			for cast := 0; cast < 5; cast++ {
				staff.CooldownRemaining = 0
				action := &Action{Type: ActionAttack, TargetPosition: &impactPosition}
				processStaffAttack(staff, nil, action, &staffWC, nil, &impacts, round*100+cast)
			}
		}
		var fields []BurnField
		ProcessStaffImpacts(&impacts, &fields, bots, nil, round*100+10)

		shieldWC := GetWeaponConfig("shield")
		for i, shield := range shieldBots {
			for attack := 0; attack < 5; attack++ {
				target := staffBots[(i+attack)%len(staffBots)]
				shield.Position = NewVec2(400, 400)
				target.Position = NewVec2(410, 400)
				shield.CooldownRemaining = 0
				processMeleeAttack(shield, target, &shieldWC, bots, nil, nil, nil, round*100+20+attack)
			}
		}

		for _, staff := range staffBots {
			if staff.RoundShotsFired != 5 || staff.RoundShotsHit != 5 {
				t.Fatalf("round %d Staff cast telemetry = %d/%d, want 5/5", round, staff.RoundShotsFired, staff.RoundShotsHit)
			}
		}
		for _, shield := range shieldBots {
			if shield.RoundShotsFired != 5 || shield.RoundShotsHit != 5 {
				t.Fatalf("round %d shield telemetry = %d/%d, want 5/5", round, shield.RoundShotsFired, shield.RoundShotsHit)
			}
		}
		AutoBalanceWeapons(context.Background(), bots, "")
	}

	staffState, _ := GetWeaponBalanceState("staff")
	afterStaff := GetWeaponConfig("staff")
	if staffState.DamageScale >= 1 {
		t.Fatalf("overperforming Staff damage scale = %.4f, want below 1", staffState.DamageScale)
	}
	if afterStaff.DamageExact >= beforeStaff.DamageExact {
		t.Fatalf("Staff exact damage did not move down: %.4f -> %.4f", beforeStaff.DamageExact, afterStaff.DamageExact)
	}
	if afterStaff.Damage != beforeStaff.Damage {
		t.Fatalf("simulation expected sub-integer correction, display damage changed %d -> %d", beforeStaff.Damage, afterStaff.Damage)
	}

	shieldState, _ := GetWeaponBalanceState("shield")
	if shieldState.DamageScale < 1 || shieldState.CooldownScale > 1 {
		t.Fatalf("comparison weapon was harmed: damage %.4f cooldown %.4f", shieldState.DamageScale, shieldState.CooldownScale)
	}
	for _, weapon := range []string{"sword", "bow", "daggers", "spear", "grapple"} {
		state, _ := GetWeaponBalanceState(weapon)
		if state.DamageScale != 1 || state.CooldownScale != 1 {
			t.Fatalf("unsampled %s changed: damage %.4f cooldown %.4f", weapon, state.DamageScale, state.CooldownScale)
		}
	}
}
