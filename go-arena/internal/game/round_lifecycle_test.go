package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestStartRoundKeepsRoundLifeStartAtCurrentTick(t *testing.T) {
	previousConfig := config.C
	previousTerrain := ActiveTerrain
	previousShape := ActiveMapShape
	previousMode := ActiveModeRules
	t.Cleanup(func() {
		config.C = previousConfig
		ActiveTerrain = previousTerrain
		ActiveMapShape = previousShape
		ActiveModeRules = previousMode
	})
	config.C.ArenaWidth = 200
	config.C.ArenaHeight = 200
	config.C.PathfindingCellSize = 20
	config.C.BotRadius = 5
	config.C.ZoneCenterX = 100
	config.C.ZoneCenterY = 100
	config.C.ZoneInitialRadius = 100
	config.C.ZoneMinRadius = 20
	config.C.TickRate = 10
	config.C.RoundDuration = 300
	config.C.TeleportPadPairs = 0
	config.C.CapturePadCount = 0
	config.C.HazardZoneCount = 0
	config.C.GameModeName = string(ModeFFA)

	engine := NewGameEngine()
	engine.NextTerrain = NewTerrainGrid(200, 200, nil, 20, 5)
	engine.NextMapShape = ShapeSquare
	engine.TickCount = 1234
	bot := &BotState{
		BotID:            "round-life",
		Name:             "Round Life",
		MaxHP:            100,
		Speed:            5,
		AttackMultiplier: 1,
		Stats:            map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
	}
	engine.Bots[bot.BotID] = bot

	engine.startRound()

	if bot.RoundLifeStartTick != engine.TickCount {
		t.Fatalf("round life started at tick %d, want current round tick %d", bot.RoundLifeStartTick, engine.TickCount)
	}
	if bot.RoundLongestLife != 0 {
		t.Fatalf("new round longest life = %d, want 0", bot.RoundLongestLife)
	}
}
