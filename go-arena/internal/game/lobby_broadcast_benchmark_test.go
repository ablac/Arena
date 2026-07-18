package game

import (
	"fmt"
	"testing"

	"arena-server/internal/config"
)

func BenchmarkTickLobby500Bots(b *testing.B) {
	originalConfig := config.C
	config.C.MinBotsToStart = 1000
	config.C.TickRate = 10
	b.Cleanup(func() { config.C = originalConfig })

	engine := &GameEngine{
		Bots:        newLobbyBroadcastTestBots(500),
		WaitingBots: make(map[string]*BotState),
		Round:       RoundState{Phase: PhaseLobby},
		TickCount:   2,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.TickCount = 2
		engine.tickLobby(&config.C)
	}
}

var benchmarkHintsSink []HintView

func BenchmarkBuildHints500Bots(b *testing.B) {
	originalTerrain := ActiveTerrain
	ActiveTerrain = NewTerrainGrid(4000, 4000, nil, 20, 0)
	b.Cleanup(func() { ActiveTerrain = originalTerrain })

	observer := &BotState{BotID: "observer", Position: NewVec2(2000, 2000), IsAlive: true}
	bots := make(map[string]*BotState, 500)
	bots[observer.BotID] = observer
	for i := 1; i < 500; i++ {
		id := fmt.Sprintf("bot-%03d", i)
		x := float64((i%40)+1) * ActiveTerrain.CellSize
		y := float64((i/40)+1) * ActiveTerrain.CellSize
		bots[id] = &BotState{
			BotID:    id,
			Position: observer.Position.Add(NewVec2(x, y)),
			IsAlive:  true,
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkHintsSink = buildHints(observer, bots, nil)
	}
}
