package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestBountySystemBasic(t *testing.T) {
	config.Load()
	bs := NewBountySystem()

	if bs.TargetID != "" {
		t.Error("new bounty system should have empty target")
	}
}

func TestBountySystemUpdate(t *testing.T) {
	config.Load()
	// threshold is 3 by default
	bs := NewBountySystem()

	bot1 := newTestBot("bot1", 100)
	bot1.KillStreak = 4
	bot1.Name = "Alpha"

	bot2 := newTestBot("bot2", 100)
	bot2.KillStreak = 2 // below threshold

	bots := map[string]*BotState{"bot1": bot1, "bot2": bot2}
	bs.Update(bots)

	if bs.TargetID != "bot1" {
		t.Errorf("expected bot1 as bounty, got %v", bs.TargetID)
	}
	if bs.TargetName != "Alpha" {
		t.Errorf("expected Alpha as bounty name, got %v", bs.TargetName)
	}
	if !bot1.IsBountyTarget {
		t.Error("bot1 should be marked as bounty target")
	}
	if bot2.IsBountyTarget {
		t.Error("bot2 should not be bounty target")
	}
}

func TestBountySystemNoBountyWhenBelowThreshold(t *testing.T) {
	config.Load()
	bs := NewBountySystem()

	bot1 := newTestBot("bot1", 100)
	bot1.KillStreak = 1

	bots := map[string]*BotState{"bot1": bot1}
	bs.Update(bots)

	if bs.TargetID != "" {
		t.Errorf("no bot should be bounty below threshold, got %v", bs.TargetID)
	}
}

func TestBountySystemDeadBotNotBounty(t *testing.T) {
	config.Load()
	bs := NewBountySystem()

	bot1 := newTestBot("bot1", 100)
	bot1.KillStreak = 10
	bot1.IsAlive = false // dead

	bots := map[string]*BotState{"bot1": bot1}
	bs.Update(bots)

	if bs.TargetID != "" {
		t.Error("dead bot should not be bounty target")
	}
}

func TestBountySystemHighestStreakWins(t *testing.T) {
	config.Load()
	bs := NewBountySystem()

	bot1 := newTestBot("bot1", 100)
	bot1.KillStreak = 4
	bot2 := newTestBot("bot2", 100)
	bot2.KillStreak = 7 // higher streak

	bots := map[string]*BotState{"bot1": bot1, "bot2": bot2}
	bs.Update(bots)

	if bs.TargetID != "bot2" {
		t.Errorf("expected bot2 (higher streak) as bounty, got %v", bs.TargetID)
	}
}

func TestBountySystemOnKill(t *testing.T) {
	config.Load()
	bs := NewBountySystem()
	bs.TargetID = "victim"
	bs.TargetName = "VictimBot"

	killer := newTestBot("killer", 100)
	victim := newTestBot("victim", 100)

	bonus := bs.OnKill(killer, victim)
	if bonus != config.C.BountyBonusPoints {
		t.Errorf("bonus=%v, want %v", bonus, config.C.BountyBonusPoints)
	}
	if killer.RoundDamageDealt != config.C.BountyBonusPoints {
		t.Errorf("killer RoundDamageDealt=%v", killer.RoundDamageDealt)
	}
}

func TestBountySystemOnKillNonTarget(t *testing.T) {
	config.Load()
	bs := NewBountySystem()
	bs.TargetID = "someoneelse"

	killer := newTestBot("killer", 100)
	victim := newTestBot("victim", 100)

	bonus := bs.OnKill(killer, victim)
	if bonus != 0 {
		t.Errorf("bonus should be 0 for non-bounty kill, got %v", bonus)
	}
}

func TestBountySystemIsBountyTarget(t *testing.T) {
	bs := NewBountySystem()
	bs.TargetID = "abc"

	if !bs.IsBountyTarget("abc") {
		t.Error("abc should be bounty target")
	}
	if bs.IsBountyTarget("xyz") {
		t.Error("xyz should not be bounty target")
	}
}

func TestBountySystemClear(t *testing.T) {
	bs := NewBountySystem()
	bs.TargetID = "abc"
	bs.TargetName = "Alpha"
	bs.Clear()

	if bs.TargetID != "" || bs.TargetName != "" {
		t.Error("bounty not cleared")
	}
}
