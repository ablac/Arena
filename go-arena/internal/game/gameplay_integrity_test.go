package game

import (
	"errors"
	"testing"

	"arena-server/internal/config"
)

func withIntegrityTestRules(t *testing.T) {
	t.Helper()
	loadTestConfig(t)
	oldTerrain := ActiveTerrain
	oldRules := ActiveModeRules
	ActiveTerrain = nil
	ActiveModeRules = ModeRules{Mode: ModeFFA}
	t.Cleanup(func() {
		ActiveTerrain = oldTerrain
		ActiveModeRules = oldRules
	})
}

func TestSubmitBotActionRejectsDuplicateStaleAndFutureTicks(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	bot := &BotState{BotID: "bot", IsAlive: true}
	engine.Bots[bot.BotID] = bot

	first := &Action{Type: ActionMove, Direction: NewVec2(1, 0)}
	if err := engine.SubmitBotAction(bot.BotID, 100, first); err != nil {
		t.Fatalf("first action rejected: %v", err)
	}
	if err := engine.SubmitBotAction(bot.BotID, 100, &Action{Type: ActionIdle}); err == nil {
		t.Fatal("duplicate client tick was accepted")
	}
	if bot.PendingAction != first {
		t.Fatalf("duplicate overwrote first action: got %+v", bot.PendingAction)
	}

	staleBot := &BotState{BotID: "stale", IsAlive: true}
	engine.Bots[staleBot.BotID] = staleBot
	if err := engine.SubmitBotAction(staleBot.BotID, 89, &Action{Type: ActionIdle}); err == nil {
		t.Fatal("action older than the replay window was accepted")
	}
	if err := engine.SubmitBotAction(staleBot.BotID, 101, &Action{Type: ActionIdle}); err == nil {
		t.Fatal("future action tick was accepted")
	}

	engine.TickCount = 1
	missingTickBot := &BotState{BotID: "missing-tick", IsAlive: true}
	engine.Bots[missingTickBot.BotID] = missingTickBot
	if err := engine.SubmitBotAction(missingTickBot.BotID, 0, &Action{Type: ActionIdle}); err == nil {
		t.Fatal("missing/zero client tick was accepted")
	}
}

func TestSubmitBotActionAcceptsOnlyFirstActionPerServerTick(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	bot := &BotState{BotID: "bot", IsAlive: true}
	engine.Bots[bot.BotID] = bot

	first := &Action{Type: ActionMove, Direction: NewVec2(1, 0)}
	if err := engine.SubmitBotAction(bot.BotID, 91, first); err != nil {
		t.Fatalf("first delayed action rejected: %v", err)
	}
	second := &Action{Type: ActionMove, Direction: NewVec2(0, 1)}
	if err := engine.SubmitBotAction(bot.BotID, 92, second); !errors.Is(err, ErrActionServerTickUsed) {
		t.Fatalf("second same-server-tick action error = %v, want %v", err, ErrActionServerTickUsed)
	}
	if bot.PendingAction != first || bot.LastClientActionTick != 91 {
		t.Fatalf("second action overwrote first: pending=%+v client_tick=%d", bot.PendingAction, bot.LastClientActionTick)
	}

	engine.TickCount++
	if err := engine.SubmitBotAction(bot.BotID, 92, second); err != nil {
		t.Fatalf("next-server-tick action rejected: %v", err)
	}
	if bot.PendingAction != second {
		t.Fatalf("next-server-tick action was not installed: %+v", bot.PendingAction)
	}
}

func TestSubmitBotActionRejectsInactiveOrDeadBotWithoutMutation(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	engine.TickCount = 100
	bot := &BotState{BotID: "bot", IsAlive: true}
	engine.Bots[bot.BotID] = bot
	action := &Action{Type: ActionIdle}

	if err := engine.SubmitBotAction(bot.BotID, 100, action); !errors.Is(err, ErrActionRoundNotActive) {
		t.Fatalf("lobby action error = %v, want %v", err, ErrActionRoundNotActive)
	}
	engine.Round.Phase = PhaseActive
	bot.IsAlive = false
	if err := engine.SubmitBotAction(bot.BotID, 100, action); !errors.Is(err, ErrActionBotNotAlive) {
		t.Fatalf("dead bot action error = %v, want %v", err, ErrActionBotNotAlive)
	}
	if bot.PendingAction != nil || bot.HasClientActionTick || bot.HasAcceptedServerTick {
		t.Fatalf("rejected action mutated bot state: %+v", bot)
	}
}

func TestProcessShovesRejectsSelfTarget(t *testing.T) {
	withIntegrityTestRules(t)
	bot := &BotState{
		BotID:         "self",
		IsAlive:       true,
		HP:            100,
		Position:      NewVec2(100, 100),
		PendingAction: &Action{Type: ActionShove, TargetID: "self"},
	}
	start := bot.Position

	ProcessShoves(map[string]*BotState{bot.BotID: bot}, nil, 10)

	if bot.Position != start || bot.StunTicks != 0 {
		t.Fatalf("self shove changed bot state: position=%v stun=%d", bot.Position, bot.StunTicks)
	}
	if bot.LastActionResult == nil || bot.LastActionResult.Success {
		t.Fatalf("self shove result = %+v, want rejected", bot.LastActionResult)
	}
}

func TestFriendlyFireOffRejectsShoveAndKnockback(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveModeRules = ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}
	attacker := &BotState{
		BotID:         "attacker",
		Team:          1,
		IsAlive:       true,
		Position:      NewVec2(100, 100),
		PendingAction: &Action{Type: ActionShove, TargetID: "ally"},
	}
	ally := &BotState{BotID: "ally", Team: 1, IsAlive: true, HP: 100, Position: NewVec2(120, 100)}
	start := ally.Position

	ProcessShoves(map[string]*BotState{attacker.BotID: attacker, ally.BotID: ally}, nil, 10)

	if ally.Position != start || ally.StunTicks != 0 || ally.HP != 100 {
		t.Fatalf("friendly shove applied control: position=%v stun=%d hp=%v", ally.Position, ally.StunTicks, ally.HP)
	}
	if attacker.LastActionResult == nil || attacker.LastActionResult.Success {
		t.Fatalf("friendly shove result = %+v, want rejected", attacker.LastActionResult)
	}

	ApplyAttributedGridKnockback(ally, attacker, attacker.Position, 2, nil, "test", 11)
	if ally.Position != start || ally.HP != 100 {
		t.Fatalf("attributed friendly knockback bypassed rules: position=%v hp=%v", ally.Position, ally.HP)
	}
	applyWallSlamDamage(ally, attacker, "test_wall_slam", 12)
	if ally.HP != 100 || ally.LastDamagedBy != "" {
		t.Fatalf("friendly wall slam bypassed rules: hp=%v attacker=%q", ally.HP, ally.LastDamagedBy)
	}
}

func TestProcessShovesUsesConfiguredGridRangeAndKnockback(t *testing.T) {
	withIntegrityTestRules(t)
	oldRange, oldKnockback := config.C.ShoveRange, config.C.ShoveKnockback
	config.C.ShoveRange = 2
	config.C.ShoveKnockback = 1
	t.Cleanup(func() {
		config.C.ShoveRange = oldRange
		config.C.ShoveKnockback = oldKnockback
	})

	attacker := &BotState{
		BotID:         "attacker",
		IsAlive:       true,
		Position:      NewVec2(100, 100),
		PendingAction: &Action{Type: ActionShove, TargetID: "target"},
	}
	target := &BotState{BotID: "target", IsAlive: true, HP: 100, Position: NewVec2(140, 100)}

	ProcessShoves(map[string]*BotState{attacker.BotID: attacker, target.BotID: target}, nil, 10)

	if attacker.LastActionResult == nil || !attacker.LastActionResult.Success {
		t.Fatalf("configured two-tile shove was rejected: %+v", attacker.LastActionResult)
	}
	if got, want := target.Position, NewVec2(160, 100); got != want {
		t.Fatalf("configured one-tile knockback moved target to %v, want %v", got, want)
	}
}

func TestRoundSpawnClearsTransientActionAndDeployableState(t *testing.T) {
	withIntegrityTestRules(t)
	bot := &BotState{
		BotID:                  "bot",
		MaxHP:                  100,
		MineCount:              3,
		GravityWellCharge:      1,
		ShoveCooldown:          1.5,
		PendingAction:          &Action{Type: ActionPlaceMine},
		LastActionResult:       &ActionResult{Action: "place_mine", Success: true},
		LastAcceptedServerTick: 44,
		HasAcceptedServerTick:  true,
	}
	grid := NewSpatialGrid(20)

	SpawnBotAt(bot, NewVec2(100, 100), grid, 50)

	if bot.MineCount != 0 || bot.GravityWellCharge != 0 || bot.ShoveCooldown != 0 {
		t.Fatalf("round resources leaked through spawn: mines=%d wells=%d shove=%v", bot.MineCount, bot.GravityWellCharge, bot.ShoveCooldown)
	}
	if bot.PendingAction != nil || bot.LastActionResult != nil || bot.HasAcceptedServerTick || bot.LastAcceptedServerTick != 0 {
		t.Fatalf("round action state leaked through spawn: action=%+v result=%+v accepted=%v/%d",
			bot.PendingAction, bot.LastActionResult, bot.HasAcceptedServerTick, bot.LastAcceptedServerTick)
	}
}

func TestGravityWellPickupChargeIsBinary(t *testing.T) {
	withIntegrityTestRules(t)
	bot := &BotState{BotID: "bot"}
	pickup := Pickup{Type: PickupGravityWell}

	applyPickupEffect(bot, pickup)
	applyPickupEffect(bot, pickup)

	if bot.GravityWellCharge != 1 {
		t.Fatalf("gravity well pickups stacked to %d charges, want 1", bot.GravityWellCharge)
	}
}

func TestUniversalGrappleRejectsSelfTarget(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	bot := &BotState{
		BotID:          "self",
		IsAlive:        true,
		HP:             100,
		Position:       NewVec2(100, 100),
		PendingAction:  &Action{Type: ActionGrapple, TargetID: "self"},
		GrappleCharges: 1,
	}
	engine.Bots[bot.BotID] = bot
	engine.Grid.Insert(bot.BotID, bot.Position)

	engine.processGrappleAbility(bot)

	if bot.LastActionResult == nil || bot.LastActionResult.Success {
		t.Fatalf("self grapple result = %+v, want rejected", bot.LastActionResult)
	}
	if bot.GrappleCharges != 1 || bot.HP != 100 || bot.StunTicks != 0 {
		t.Fatalf("self grapple mutated state: charges=%d hp=%v stun=%d", bot.GrappleCharges, bot.HP, bot.StunTicks)
	}
}

func TestUniversalGrappleAnchorRequiresLineOfSight(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	engine.Arena.Obstacles = []Obstacle{{X: 145, Y: 80, Width: 20, Height: 40}}
	anchor := NewVec2(200, 100)
	bot := &BotState{
		BotID:          "grappler",
		IsAlive:        true,
		Position:       NewVec2(100, 100),
		PendingAction:  &Action{Type: ActionGrapple, TargetPosition: &anchor},
		GrappleCharges: 1,
	}
	start := bot.Position
	engine.Bots[bot.BotID] = bot
	engine.Grid.Insert(bot.BotID, bot.Position)

	engine.processGrappleAbility(bot)

	if bot.LastActionResult == nil || bot.LastActionResult.Success {
		t.Fatalf("blocked anchor grapple result = %+v, want rejected", bot.LastActionResult)
	}
	if bot.Position != start || bot.GrappleCharges != 1 {
		t.Fatalf("blocked anchor grapple mutated state: position=%v charges=%d", bot.Position, bot.GrappleCharges)
	}
}

func TestCleaveSecondaryRequiresLineOfSight(t *testing.T) {
	withIntegrityTestRules(t)
	bot := &BotState{BotID: "attacker", IsAlive: true, Weapon: "sword", AttackMultiplier: 1, Position: NewVec2(100, 100)}
	primary := &BotState{BotID: "primary", IsAlive: true, HP: 100, Position: NewVec2(120, 100)}
	secondary := &BotState{BotID: "secondary", IsAlive: true, HP: 100, Position: NewVec2(100, 140)}
	bots := map[string]*BotState{bot.BotID: bot, primary.BotID: primary, secondary.BotID: secondary}
	grid := NewSpatialGrid(20)
	for _, candidate := range bots {
		grid.Insert(candidate.BotID, candidate.Position)
	}
	obstacles := []Obstacle{{X: 90, Y: 117, Width: 20, Height: 8}}
	wc := GetWeaponConfig("sword")
	start := secondary.Position

	processCleave(bot, primary, &wc, bots, obstacles, grid, 10)

	if secondary.HP != 100 || secondary.Position != start {
		t.Fatalf("cleave crossed blocked LOS: hp=%v position=%v", secondary.HP, secondary.Position)
	}
}

func TestAntiTeamOnlyResetsOnSuccessfulCombatCommitment(t *testing.T) {
	withIntegrityTestRules(t)
	engine := NewGameEngine()
	attacker := &BotState{
		BotID:            "attacker",
		PendingAction:    &Action{Type: ActionAttack, TargetID: "target"},
		LastActionResult: &ActionResult{Action: "attack", Success: false, Target: "target"},
	}
	target := &BotState{BotID: "target"}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[target.BotID] = target
	key := pairKey(attacker.BotID, target.BotID)
	engine.AntiTeam.proximity[key] = 12

	engine.recordSuccessfulCombatCommitments()
	if got := engine.AntiTeam.proximity[key]; got != 12 {
		t.Fatalf("failed attack reset anti-team counter: got %d want 12", got)
	}

	attacker.LastActionResult = &ActionResult{Action: "attack", Success: true, Target: "target"}
	engine.recordSuccessfulCombatCommitments()
	if _, ok := engine.AntiTeam.proximity[key]; ok {
		t.Fatal("successful combat commitment did not reset anti-team counter")
	}
}

func TestFriendlyFireOffRejectsDirectMeleeControl(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveModeRules = ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}
	attacker := &BotState{
		BotID:            "attacker",
		Team:             1,
		IsAlive:          true,
		Weapon:           "sword",
		AttackMultiplier: 1,
		Position:         NewVec2(100, 100),
		PendingAction:    &Action{Type: ActionAttack, TargetID: "ally"},
	}
	ally := &BotState{BotID: "ally", Team: 1, IsAlive: true, HP: 100, Position: NewVec2(120, 100)}
	bots := map[string]*BotState{attacker.BotID: attacker, ally.BotID: ally}
	grid := NewSpatialGrid(20)
	grid.Insert(attacker.BotID, attacker.Position)
	grid.Insert(ally.BotID, ally.Position)
	var projectiles []Projectile
	var impacts []StaffImpact
	var events []ArenaEvent
	start := ally.Position

	ProcessCombat(bots, nil, &projectiles, &impacts, &events, grid, 10, 0.1)

	if ally.HP != 100 || ally.Position != start || ally.StunTicks != 0 {
		t.Fatalf("friendly melee applied damage/control: hp=%v position=%v stun=%d", ally.HP, ally.Position, ally.StunTicks)
	}
	if attacker.LastActionResult == nil || attacker.LastActionResult.Success {
		t.Fatalf("friendly melee result = %+v, want rejected", attacker.LastActionResult)
	}
}

func TestFriendlyFireOffUniversalGrappleDoesNotControlAlly(t *testing.T) {
	withIntegrityTestRules(t)
	ActiveModeRules = ModeRules{Mode: ModeTeamBattle, TeamCount: 2, FriendlyFire: false}
	engine := NewGameEngine()
	attacker := &BotState{
		BotID:            "attacker",
		Team:             1,
		IsAlive:          true,
		AttackMultiplier: 1,
		Position:         NewVec2(100, 100),
		PendingAction:    &Action{Type: ActionGrapple, TargetID: "ally"},
		GrappleCharges:   1,
	}
	ally := &BotState{BotID: "ally", Team: 1, IsAlive: true, HP: 100, Position: NewVec2(140, 100)}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[ally.BotID] = ally
	engine.Grid.Insert(attacker.BotID, attacker.Position)
	engine.Grid.Insert(ally.BotID, ally.Position)
	start := ally.Position

	engine.processGrappleAbility(attacker)

	if attacker.LastActionResult == nil || attacker.LastActionResult.Success {
		t.Fatalf("friendly grapple result = %+v, want rejected", attacker.LastActionResult)
	}
	if ally.HP != 100 || ally.Position != start || ally.StunTicks != 0 || attacker.GrappleCharges != 1 {
		t.Fatalf("friendly grapple applied control: ally_hp=%v position=%v stun=%d charges=%d", ally.HP, ally.Position, ally.StunTicks, attacker.GrappleCharges)
	}
}

func TestStaffRejectsAmbiguousTargetWithoutAntiTeamCredit(t *testing.T) {
	withIntegrityTestRules(t)
	position := NewVec2(120, 100)
	attacker := &BotState{
		BotID:         "staff",
		Weapon:        "staff",
		IsAlive:       true,
		Position:      NewVec2(100, 100),
		PendingAction: &Action{Type: ActionAttack, TargetID: "target", TargetPosition: &position},
	}
	target := &BotState{BotID: "target", Weapon: "sword", IsAlive: true, Position: position, HP: 100}
	bots := map[string]*BotState{attacker.BotID: attacker, target.BotID: target}
	var impacts []StaffImpact

	ProcessCombat(bots, nil, nil, &impacts, nil, NewSpatialGrid(20), 10, 0.05)

	if attacker.LastActionResult == nil || attacker.LastActionResult.Success {
		t.Fatalf("ambiguous staff attack result = %+v, want rejected", attacker.LastActionResult)
	}
	if len(impacts) != 0 || attacker.RoundShotsFired != 0 {
		t.Fatalf("ambiguous staff attack committed impacts=%d shots=%d", len(impacts), attacker.RoundShotsFired)
	}

	tracker := NewAntiTeamTracker()
	key := pairKey(attacker.BotID, target.BotID)
	tracker.proximity[key] = 17
	engine := &GameEngine{Bots: bots, AntiTeam: tracker}
	engine.recordSuccessfulCombatCommitments()
	if got := tracker.proximity[key]; got != 17 {
		t.Fatalf("ambiguous staff request reset anti-team proximity to %d", got)
	}
}

func TestActionReplayWindowTracksServerTickRate(t *testing.T) {
	withIntegrityTestRules(t)
	oldTickRate := config.C.TickRate
	config.C.TickRate = 20
	t.Cleanup(func() { config.C.TickRate = oldTickRate })
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	bot := &BotState{BotID: "bot", IsAlive: true}
	engine.Bots[bot.BotID] = bot

	if err := engine.SubmitBotAction(bot.BotID, 80, &Action{Type: ActionIdle}); err != nil {
		t.Fatalf("one-second-late action rejected: %v", err)
	}
}
