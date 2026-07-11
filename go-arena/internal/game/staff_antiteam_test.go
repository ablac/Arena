package game

import "testing"

func TestStaffPlacementDoesNotReceiveCastOnlyAntiTeamCredit(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	attacker := &BotState{
		BotID:            "staff",
		Weapon:           "staff",
		PendingAction:    &Action{Type: ActionAttack, TargetID: "target"},
		LastActionResult: &ActionResult{Action: "attack", Success: true, Target: "target"},
	}
	target := &BotState{BotID: "target"}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[target.BotID] = target
	key := pairKey(attacker.BotID, target.BotID)
	engine.AntiTeam.proximity[key] = 12

	engine.recordSuccessfulCombatCommitments()

	if got := engine.AntiTeam.proximity[key]; got != 12 {
		t.Fatalf("staff placement received anti-team credit before damage: got %d want 12", got)
	}
}

func TestStaffDetonationRecordsOnlyActualDamagePairsForAntiTeam(t *testing.T) {
	withIntegrityTestRules(t)
	attacker := &BotState{BotID: "staff", Weapon: "staff", IsAlive: true, Position: NewVec2(100, 100), AttackMultiplier: 1}
	victim := &BotState{BotID: "victim", IsAlive: true, HP: 100, Position: NewVec2(120, 100)}
	dodging := &BotState{BotID: "dodging", IsAlive: true, HP: 100, Position: NewVec2(125, 100), InvulnTicks: 1}
	bots := map[string]*BotState{attacker.BotID: attacker, victim.BotID: victim, dodging.BotID: dodging}
	tracker := NewAntiTeamTracker()
	victimKey := pairKey(attacker.BotID, victim.BotID)
	dodgingKey := pairKey(attacker.BotID, dodging.BotID)
	tracker.proximity[victimKey] = 12
	tracker.proximity[dodgingKey] = 13
	impacts := []StaffImpact{{
		OwnerID:    attacker.BotID,
		Position:   victim.Position,
		Radius:     2,
		Damage:     10,
		TicksLeft:  1,
		AttackMult: 1,
	}}
	var fields []BurnField

	ProcessStaffImpacts(&impacts, &fields, bots, tracker, 20)

	if _, ok := tracker.proximity[victimKey]; ok {
		t.Fatal("actual staff detonation damage did not reset the affected pair")
	}
	if got := tracker.proximity[dodgingKey]; got != 13 {
		t.Fatalf("zero-damage dodge contact received anti-team credit: got %d want 13", got)
	}
}

func TestStaffBurnRecordsActualDamagePairForAntiTeam(t *testing.T) {
	withIntegrityTestRules(t)
	attacker := &BotState{BotID: "staff", Weapon: "staff", IsAlive: true, Position: NewVec2(100, 100), AttackMultiplier: 1}
	victim := &BotState{BotID: "victim", IsAlive: true, HP: 100, Position: NewVec2(120, 100)}
	bots := map[string]*BotState{attacker.BotID: attacker, victim.BotID: victim}
	tracker := NewAntiTeamTracker()
	key := pairKey(attacker.BotID, victim.BotID)
	tracker.proximity[key] = 12
	fields := []BurnField{{
		OwnerID:      attacker.BotID,
		Position:     victim.Position,
		Radius:       1,
		Damage:       5,
		AttackMult:   1,
		TicksLeft:    2,
		TickInterval: 1,
	}}

	ProcessBurnFields(&fields, bots, tracker, 21)

	if _, ok := tracker.proximity[key]; ok {
		t.Fatal("actual staff burn damage did not reset the affected pair")
	}
}
