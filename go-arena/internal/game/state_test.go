package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestResetRoundStats(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.RoundKills = 5
	bot.RoundDeaths = 2
	bot.RoundDamageDealt = 300
	bot.RoundDamageTaken = 150
	bot.RoundDistance = 500
	bot.RoundShotsFired = 10
	bot.RoundShotsHit = 8
	bot.RoundLongestLife = 100
	bot.RoundPickups = 3
	bot.PersistedKills = 5
	bot.PersistedDeaths = 2

	bot.ResetRoundStats()

	if bot.RoundKills != 0 || bot.RoundDeaths != 0 {
		t.Errorf("kills/deaths not reset")
	}
	if bot.RoundDamageDealt != 0 || bot.RoundDamageTaken != 0 {
		t.Errorf("damage not reset")
	}
	if bot.RoundDistance != 0 {
		t.Errorf("distance not reset")
	}
	if bot.RoundPickups != 0 {
		t.Errorf("pickups not reset")
	}
	if bot.PersistedKills != 0 || bot.PersistedDeaths != 0 {
		t.Errorf("persisted stats not reset")
	}
}

func TestClearTickFeedback(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)
	bot.HitsReceived = []HitRecord{{AttackerID: "x", Damage: 10, Weapon: "sword"}}
	bot.LastActionResult = &ActionResult{Action: "attack", Success: true}
	bot.PendingAction = &Action{Type: ActionAttack}

	bot.ClearTickFeedback()

	if bot.HitsReceived != nil {
		t.Errorf("HitsReceived not cleared")
	}
	if bot.LastActionResult != nil {
		t.Errorf("LastActionResult not cleared")
	}
	if bot.PendingAction != nil {
		t.Errorf("PendingAction not cleared")
	}
}

func TestComputeDerivedStats(t *testing.T) {
	config.Load()
	stats := map[string]int{
		"hp": 5, "speed": 5, "attack": 5, "defense": 5,
	}
	ds := ComputeDerivedStats(stats, "sword")

	if ds.MaxHP <= 0 {
		t.Errorf("MaxHP=%v, want >0", ds.MaxHP)
	}
	if ds.MoveSpeed <= 0 {
		t.Errorf("MoveSpeed=%v, want >0", ds.MoveSpeed)
	}
	if ds.AttackMult <= 0 {
		t.Errorf("AttackMult=%v, want >0", ds.AttackMult)
	}
}

func TestComputeDerivedStatsZeroStats(t *testing.T) {
	config.Load()
	stats := map[string]int{
		"hp": 0, "speed": 0, "attack": 0, "defense": 0,
	}
	ds := ComputeDerivedStats(stats, "bow")
	// Base values should still be positive from config defaults
	if ds.MaxHP <= 0 {
		t.Errorf("zero stats should still have base HP, got %v", ds.MaxHP)
	}
}
