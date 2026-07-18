package game

import (
	"reflect"
	"testing"
)

func TestBuildHintsReportsDistanceInGridTiles(t *testing.T) {
	originalTerrain := ActiveTerrain
	ActiveTerrain = NewTerrainGrid(800, 800, nil, 20, 0)
	t.Cleanup(func() { ActiveTerrain = originalTerrain })

	bot := &BotState{BotID: "observer", Position: NewVec2(100, 100), IsAlive: true}
	enemy := &BotState{BotID: "enemy", Position: NewVec2(300, 100), IsAlive: true}
	pickup := Pickup{ID: "heal", Type: PickupHealthPack, Position: NewVec2(100, 300)}

	hints := buildHints(bot, map[string]*BotState{
		bot.BotID:   bot,
		enemy.BotID: enemy,
	}, []Pickup{pickup})

	byType := make(map[string]*HintView, len(hints))
	for i := range hints {
		byType[hints[i].HintType] = &hints[i]
	}

	for _, hintType := range []string{"bot", "pickup"} {
		hint := byType[hintType]
		if hint == nil {
			t.Fatalf("missing %s hint in %#v", hintType, hints)
		}
		if got := hint.Distance; got != 10 {
			t.Errorf("%s hint distance = %v, want 10 grid tiles for 200 world units with 20-unit cells", hintType, got)
		}
	}

	botHint := byType["bot"]
	direction := botHint.Direction
	distance := botHint.Distance
	originGrid := bot.Position.Scale(1 / ActiveTerrain.CellSize)
	reconstructedTarget := originGrid.Add(NewVec2(direction[0], direction[1]).Scale(distance))
	wantTarget := enemy.Position.Scale(1 / ActiveTerrain.CellSize)
	if reconstructedTarget != wantTarget {
		t.Fatalf("hint reconstructs grid target %v, want %v; distance must not retain the 20x world-unit scale", reconstructedTarget, wantTarget)
	}
}

func TestBuildHintsDeterministicallySelectsThreeEqualDistanceBots(t *testing.T) {
	originalTerrain := ActiveTerrain
	ActiveTerrain = NewTerrainGrid(800, 800, nil, 20, 0)
	t.Cleanup(func() { ActiveTerrain = originalTerrain })

	observer := &BotState{BotID: "observer", Position: NewVec2(400, 400), IsAlive: true}
	offsets := []Vec2{
		NewVec2(10, 0),
		NewVec2(8, 6),
		NewVec2(6, 8),
		NewVec2(0, 10),
		NewVec2(-6, 8),
		NewVec2(-8, 6),
		NewVec2(-10, 0),
		NewVec2(-8, -6),
	}
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	candidates := make([]*BotState, len(ids))
	for i, id := range ids {
		candidates[i] = &BotState{
			BotID:    id,
			Position: observer.Position.Add(offsets[i].Scale(ActiveTerrain.CellSize)),
			IsAlive:  true,
		}
	}

	want := [][2]float64{{1, 0}, {0.8, 0.6}, {0.6, 0.8}}
	for iteration := 0; iteration < 256; iteration++ {
		allBots := map[string]*BotState{observer.BotID: observer}
		for i := range candidates {
			candidate := candidates[(i+iteration)%len(candidates)]
			allBots[candidate.BotID] = candidate
		}

		hints := buildHints(observer, allBots, nil)
		got := make([][2]float64, 0, 3)
		for _, hint := range hints {
			if hint.HintType != "bot" {
				continue
			}
			got = append(got, hint.Direction)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d bot hint directions = %v, want deterministic BotID tie order %v", iteration, got, want)
		}
	}
}
