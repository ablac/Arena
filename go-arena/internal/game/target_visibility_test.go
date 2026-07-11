package game

import (
	"errors"
	"testing"

	"arena-server/internal/config"
)

func TestSubmitBotActionRejectsTargetOutsideCurrentFog(t *testing.T) {
	withIntegrityTestRules(t)
	oldFogRadius := config.C.FogRadius
	config.C.FogRadius = 7
	t.Cleanup(func() { config.C.FogRadius = oldFogRadius })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	attacker := &BotState{BotID: "attacker", IsAlive: true, Position: NewVec2(100, 100)}
	target := &BotState{BotID: "target", IsAlive: true, Position: NewVec2(260, 100)}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[target.BotID] = target
	engine.Grid.Insert(attacker.BotID, attacker.Position)
	engine.Grid.Insert(target.BotID, target.Position)

	action := &Action{Type: ActionGrapple, TargetID: target.BotID}
	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 100, action); err == nil {
		t.Fatal("target-ID action outside current fog was accepted")
	}
	if attacker.PendingAction != nil || attacker.HasClientActionTick {
		t.Fatalf("rejected target mutated action state: action=%+v has_tick=%v", attacker.PendingAction, attacker.HasClientActionTick)
	}
}

func TestActionTickValidationPrecedesTargetVisibility(t *testing.T) {
	withIntegrityTestRules(t)
	oldFogRadius := config.C.FogRadius
	config.C.FogRadius = 7
	t.Cleanup(func() { config.C.FogRadius = oldFogRadius })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	attacker := &BotState{BotID: "attacker", IsAlive: true, Position: NewVec2(100, 100)}
	target := &BotState{BotID: "target", IsAlive: true, Position: NewVec2(260, 100)}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[target.BotID] = target
	action := &Action{Type: ActionAttack, TargetID: target.BotID}

	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 101, action); !errors.Is(err, ErrActionTickFuture) {
		t.Fatalf("future invisible action error = %v, want ErrActionTickFuture", err)
	}
	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 89, action); !errors.Is(err, ErrActionTickStale) {
		t.Fatalf("stale invisible action error = %v, want ErrActionTickStale", err)
	}

	target.Position = NewVec2(120, 100)
	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 100, action); err != nil {
		t.Fatalf("setup visible action rejected: %v", err)
	}
	target.Position = NewVec2(260, 100)
	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 100, action); !errors.Is(err, ErrActionTickDuplicate) {
		t.Fatalf("duplicate invisible action error = %v, want ErrActionTickDuplicate", err)
	}
}

func TestSubmitBotActionAllowsLiveBountyTargetOutsideFog(t *testing.T) {
	withIntegrityTestRules(t)
	oldFogRadius := config.C.FogRadius
	config.C.FogRadius = 7
	t.Cleanup(func() { config.C.FogRadius = oldFogRadius })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	attacker := &BotState{BotID: "attacker", IsAlive: true, Position: NewVec2(100, 100)}
	target := &BotState{BotID: "target", IsAlive: true, IsBountyTarget: true, Position: NewVec2(260, 100)}
	engine.Bots[attacker.BotID] = attacker
	engine.Bots[target.BotID] = target
	engine.Bounty.TargetID = target.BotID

	action := &Action{Type: ActionGrapple, TargetID: target.BotID}
	if err := engine.SubmitBotActionForSession(attacker.BotID, attacker, 100, action); err != nil {
		t.Fatalf("live bounty target outside fog was rejected: %v", err)
	}
	if attacker.PendingAction != action {
		t.Fatalf("accepted bounty action was not installed: got %+v", attacker.PendingAction)
	}
}
