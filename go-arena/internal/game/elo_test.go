package game

import (
	"testing"

	"arena-server/internal/config"
)

func setupEloTest(t *testing.T) {
	t.Helper()
	loadTestConfig(t)
	config.C.EloKFactor = 32
	config.C.EloMin = 100
	config.C.EloMax = 3000
}

func TestCalculateEloChangeIsMatchedTransfer(t *testing.T) {
	setupEloTest(t)

	for _, tc := range []struct {
		name              string
		killerElo, victim int
	}{
		{name: "even", killerElo: 1000, victim: 1000},
		{name: "underdog", killerElo: 700, victim: 1300},
		{name: "favorite", killerElo: 1300, victim: 700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gain, loss := CalculateEloChange(tc.killerElo, tc.victim)
			if gain != loss {
				t.Fatalf("gain=%d loss=%d; rating transfer is not zero-sum", gain, loss)
			}
			if gain < 0 {
				t.Fatalf("negative transfer %d", gain)
			}
		})
	}
}

func TestCalculateEloChangeDoesNotForceFavoriteReward(t *testing.T) {
	setupEloTest(t)
	gain, loss := CalculateEloChange(3000, 100)
	if gain != 0 || loss != 0 {
		t.Fatalf("overwhelming favorite transfer = %d/%d, want 0/0", gain, loss)
	}
}

func TestApplyEloChangePreservesTotal(t *testing.T) {
	setupEloTest(t)
	killer := &BotState{Elo: 1000}
	victim := &BotState{Elo: 1000}
	before := killer.Elo + victim.Elo

	ApplyEloChange(killer, victim)

	if killer.Elo+victim.Elo != before {
		t.Fatalf("total rating changed: before=%d after=%d", before, killer.Elo+victim.Elo)
	}
	if killer.Elo != 1016 || victim.Elo != 984 {
		t.Fatalf("even match = %d/%d, want 1016/984", killer.Elo, victim.Elo)
	}
}

func TestApplyEloChangeShrinksAtBounds(t *testing.T) {
	setupEloTest(t)

	t.Run("victim floor", func(t *testing.T) {
		killer := &BotState{Elo: 100}
		victim := &BotState{Elo: 105}
		ApplyEloChange(killer, victim)
		if killer.Elo != 105 || victim.Elo != 100 {
			t.Fatalf("floor transfer = %d/%d, want 105/100", killer.Elo, victim.Elo)
		}
	})

	t.Run("killer ceiling", func(t *testing.T) {
		killer := &BotState{Elo: 2995}
		victim := &BotState{Elo: 3000}
		ApplyEloChange(killer, victim)
		if killer.Elo != 3000 || victim.Elo != 2995 {
			t.Fatalf("ceiling transfer = %d/%d, want 3000/2995", killer.Elo, victim.Elo)
		}
	})
}

func TestApplyEloChangeClampsLegacyInflation(t *testing.T) {
	setupEloTest(t)
	killer := &BotState{Elo: 193480}
	victim := &BotState{Elo: -50}

	ApplyEloChange(killer, victim)

	if killer.Elo != 3000 || victim.Elo != 100 {
		t.Fatalf("legacy ratings not clamped: %d/%d", killer.Elo, victim.Elo)
	}
}

func TestClampEloUsesOneFallbackPairForInvertedConfig(t *testing.T) {
	setupEloTest(t)
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.EloMin = 5000
	config.C.EloMax = 2000

	if got := ClampElo(-50); got != config.DefaultEloMin {
		t.Fatalf("low rating with inverted bounds = %d, want %d", got, config.DefaultEloMin)
	}
	if got := ClampElo(9999); got != config.DefaultEloMax {
		t.Fatalf("high rating with inverted bounds = %d, want %d", got, config.DefaultEloMax)
	}
}

func TestResetBotLeaderboardStateClampsStartingElo(t *testing.T) {
	setupEloTest(t)
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.EloMin = 800
	config.C.EloMax = 1200

	for _, tt := range []struct {
		name     string
		starting int
		want     int
	}{
		{name: "above ceiling", starting: 5000, want: 1200},
		{name: "below floor", starting: 200, want: 800},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config.C.EloStarting = tt.starting
			bot := &BotState{Elo: 1000}
			resetBotLeaderboardState(bot)
			if bot.Elo != tt.want {
				t.Fatalf("reset Elo = %d, want %d", bot.Elo, tt.want)
			}
		})
	}
}
