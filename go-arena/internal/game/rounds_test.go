package game

import (
	"math"
	"testing"

	"arena-server/internal/config"
)

func TestShouldEndRoundNoBots(t *testing.T) {
	config.Load()
	bots := map[string]*BotState{}
	round := &RoundState{StartTick: 0}
	if !ShouldEndRound(bots, round, 1) {
		t.Error("should end round with no bots")
	}
}

func TestShouldEndRoundTimeExpired(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot2 := newTestBot("b2", 100)
	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	round := &RoundState{StartTick: 0}
	// Tick beyond round duration
	maxTick := int(config.C.RoundDuration*float64(config.C.TickRate)) + 1
	if !ShouldEndRound(bots, round, maxTick) {
		t.Error("should end round when time expired")
	}
}

func TestShouldEndRoundOneAlive(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot2 := newTestBot("b2", 100)
	bot2.IsAlive = false // dead
	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	round := &RoundState{StartTick: 0}
	if !ShouldEndRound(bots, round, 1) {
		t.Error("should end round with only 1 alive bot")
	}
}

func TestShouldEndRoundContinues(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot2 := newTestBot("b2", 100)
	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	round := &RoundState{StartTick: 0}
	if ShouldEndRound(bots, round, 1) {
		t.Error("should not end round with 2 alive bots and time remaining")
	}
}

func TestDetermineWinnerLastAlive(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot2 := newTestBot("b2", 100)
	bot2.IsAlive = false
	bot1.Name = "Alpha"
	bots := map[string]*BotState{"b1": bot1, "b2": bot2}

	wID, wName := DetermineWinner(bots)
	if wID != "b1" {
		t.Errorf("winner ID=%v, want b1", wID)
	}
	if wName != "Alpha" {
		t.Errorf("winner name=%v, want Alpha", wName)
	}
}

func TestDetermineWinnerMostKills(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 0)
	bot1.IsAlive = false
	bot1.RoundKills = 5
	bot1.Name = "Alpha"

	bot2 := newTestBot("b2", 0)
	bot2.IsAlive = false
	bot2.RoundKills = 3
	bot2.Name = "Beta"

	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	wID, wName := DetermineWinner(bots)
	if wID != "b1" {
		t.Errorf("winner should be b1 (more kills), got %v", wID)
	}
	_ = wName
}

func TestCalculateAwardsMVP(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot1.RoundKills = 5
	bot1.Name = "Alpha"
	bot2 := newTestBot("b2", 100)
	bot2.RoundKills = 2
	bot2.Name = "Beta"

	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	awards := CalculateAwards(bots)

	if awards["MVP"] != "Alpha" {
		t.Errorf("MVP=%v, want Alpha", awards["MVP"])
	}
}

func TestCalculateAwardsNoMVP(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot1.RoundKills = 0
	bots := map[string]*BotState{"b1": bot1}
	awards := CalculateAwards(bots)
	if _, ok := awards["MVP"]; ok {
		t.Error("MVP should not be awarded with 0 kills")
	}
}

func TestCalculateAwardsSpeedDemon(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot1.RoundDistance = 500
	bot1.Name = "Speedy"
	bots := map[string]*BotState{"b1": bot1}
	awards := CalculateAwards(bots)
	if awards["Speed Demon"] != "Speedy" {
		t.Errorf("Speed Demon=%v, want Speedy", awards["Speed Demon"])
	}
}

func TestCalculateAwardsBerserker(t *testing.T) {
	config.Load()
	bot1 := newTestBot("b1", 100)
	bot1.RoundDamageDealt = 1000
	bot1.Name = "Berserker1"
	bots := map[string]*BotState{"b1": bot1}
	awards := CalculateAwards(bots)
	if awards["Berserker"] != "Berserker1" {
		t.Errorf("Berserker=%v, want Berserker1", awards["Berserker"])
	}
}

func TestCalculateEloChange(t *testing.T) {
	config.Load()
	gain, loss := CalculateEloChange(1000, 1000)
	// Equal Elo — expected value is 0.5, so gain ≈ loss ≈ K/2
	if gain <= 0 || loss <= 0 {
		t.Errorf("gain=%d loss=%d should both be > 0", gain, loss)
	}
}

func TestCalculateEloChangeFavorite(t *testing.T) {
	config.Load()
	// Killer has much higher Elo than victim → small gain, larger loss
	gain, loss := CalculateEloChange(2000, 1000)
	if gain >= loss {
		t.Errorf("favorite killing underdog: gain=%d should be < loss=%d", gain, loss)
	}
}

func TestCalculateEloChangeUnderdog(t *testing.T) {
	config.Load()
	// Underdog kills favorite → large gain, small loss
	gain, loss := CalculateEloChange(1000, 2000)
	if gain <= loss {
		t.Errorf("underdog killing favorite: gain=%d should be > loss=%d", gain, loss)
	}
}

func TestApplyEloChange(t *testing.T) {
	config.Load()
	killer := newTestBot("k", 100)
	killer.Elo = 1000
	victim := newTestBot("v", 100)
	victim.Elo = 1000

	ApplyEloChange(killer, victim)

	if killer.Elo <= 1000 {
		t.Errorf("killer Elo should increase, got %v", killer.Elo)
	}
	if victim.Elo >= 1000 {
		t.Errorf("victim Elo should decrease, got %v", victim.Elo)
	}
}

func TestApplyEloChangeMinimum(t *testing.T) {
	config.Load()
	killer := newTestBot("k", 100)
	killer.Elo = 3000
	victim := newTestBot("v", 100)
	victim.Elo = config.C.EloMin + 1

	ApplyEloChange(killer, victim)

	if victim.Elo < config.C.EloMin {
		t.Errorf("victim Elo %v below minimum %v", victim.Elo, config.C.EloMin)
	}
}

// Ensure math import doesn't cause issues
var _ = math.Abs
