package game

import "testing"

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

	byType := make(map[string]map[string]interface{}, len(hints))
	for _, hint := range hints {
		hintType, _ := hint["hint_type"].(string)
		byType[hintType] = hint
	}

	for _, hintType := range []string{"bot", "pickup"} {
		hint := byType[hintType]
		if hint == nil {
			t.Fatalf("missing %s hint in %#v", hintType, hints)
		}
		if got, _ := hint["distance"].(float64); got != 10 {
			t.Errorf("%s hint distance = %v, want 10 grid tiles for 200 world units with 20-unit cells", hintType, got)
		}
	}

	botHint := byType["bot"]
	direction, ok := botHint["direction"].([2]float64)
	if !ok {
		t.Fatalf("bot hint direction = %T, want [2]float64", botHint["direction"])
	}
	distance, _ := botHint["distance"].(float64)
	originGrid := bot.Position.Scale(1 / ActiveTerrain.CellSize)
	reconstructedTarget := originGrid.Add(NewVec2(direction[0], direction[1]).Scale(distance))
	wantTarget := enemy.Position.Scale(1 / ActiveTerrain.CellSize)
	if reconstructedTarget != wantTarget {
		t.Fatalf("hint reconstructs grid target %v, want %v; distance must not retain the 20x world-unit scale", reconstructedTarget, wantTarget)
	}
}
