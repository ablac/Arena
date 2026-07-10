package game

import (
	"math"
	"testing"

	"arena-server/internal/config"
)

func setMovementTestConfig(t *testing.T) {
	t.Helper()
	oldBudget := config.C.StatBudget
	oldBase := config.C.StatSpeedBase
	oldPerPoint := config.C.StatSpeedPerPoint
	config.C.StatBudget = 20
	config.C.StatSpeedBase = 3
	config.C.StatSpeedPerPoint = 0.5
	t.Cleanup(func() {
		config.C.StatBudget = oldBudget
		config.C.StatSpeedBase = oldBase
		config.C.StatSpeedPerPoint = oldPerPoint
	})
}

func openMovementTerrain(width, height int) *TerrainGrid {
	cells := make([][]byte, width)
	for x := range cells {
		cells[x] = make([]byte, height)
		for y := range cells[x] {
			cells[x][y] = '.'
		}
	}
	return &TerrainGrid{Width: width, Height: height, CellSize: 20, Cells: cells}
}

func TestTerrainMovementUsesSpeedStat(t *testing.T) {
	setMovementTestConfig(t)
	oldTerrain := ActiveTerrain
	ActiveTerrain = openMovementTerrain(24, 6)
	t.Cleanup(func() { ActiveTerrain = oldTerrain })

	dir := Vec2{1, 0}
	low := &BotState{
		BotID: "low", IsAlive: true, Speed: 3.5,
		Position:      ActiveTerrain.GridToWorld([2]int{1, 1}),
		PendingAction: &Action{Type: ActionMove, Direction: dir},
	}
	high := &BotState{
		BotID: "high", IsAlive: true, Speed: 8,
		Position:      ActiveTerrain.GridToWorld([2]int{1, 3}),
		PendingAction: &Action{Type: ActionMove, Direction: dir},
	}
	bots := map[string]*BotState{low.BotID: low, high.BotID: high}
	grid := NewSpatialGrid(20)

	for tick := 0; tick < 12; tick++ {
		ProcessMovement(bots, nil, grid, nil, 0.1)
	}

	lowCell := ActiveTerrain.WorldToGrid(low.Position)
	highCell := ActiveTerrain.WorldToGrid(high.Position)
	if highCell[0] <= lowCell[0] {
		t.Fatalf("high-speed bot reached column %d, low-speed bot reached %d; speed stat had no movement effect", highCell[0], lowCell[0])
	}
	if got := highCell[0] - 1; got < 7 {
		t.Errorf("high-speed bot moved only %d cells in 12 ticks, want at least 7", got)
	}
	if got := lowCell[0] - 1; got > 4 {
		t.Errorf("low-speed bot moved %d cells in 12 ticks, want at most 4", got)
	}
}

func TestHighSpeedTerrainMovementChecksEveryCell(t *testing.T) {
	setMovementTestConfig(t)
	oldTerrain := ActiveTerrain
	ActiveTerrain = openMovementTerrain(12, 4)
	ActiveTerrain.Cells[4][1] = '#'
	t.Cleanup(func() { ActiveTerrain = oldTerrain })

	dir := Vec2{1, 0}
	bot := &BotState{
		BotID: "sprinter", IsAlive: true, Speed: 100,
		Position:      ActiveTerrain.GridToWorld([2]int{1, 1}),
		PendingAction: &Action{Type: ActionMove, Direction: dir},
	}
	grid := NewSpatialGrid(20)
	ProcessMovement(map[string]*BotState{bot.BotID: bot}, nil, grid, nil, 0.1)

	cell := ActiveTerrain.WorldToGrid(bot.Position)
	if cell != [2]int{3, 1} {
		t.Fatalf("high-speed move ended at %v, want [3 1] immediately before wall", cell)
	}
	if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
		t.Fatalf("high-speed bot tunneled into blocked cell %v", cell)
	}
}

func TestMoveToUsesSameSpeedPacing(t *testing.T) {
	setMovementTestConfig(t)
	oldTerrain := ActiveTerrain
	ActiveTerrain = openMovementTerrain(24, 6)
	t.Cleanup(func() { ActiveTerrain = oldTerrain })

	lowTarget := Vec2{20, 2}
	highTarget := Vec2{20, 4}
	low := &BotState{
		BotID: "low-path", IsAlive: true, Speed: 3.5,
		Position:      ActiveTerrain.GridToWorld([2]int{1, 1}),
		PendingAction: &Action{Type: ActionMoveTo, TargetPosition: &lowTarget},
	}
	high := &BotState{
		BotID: "high-path", IsAlive: true, Speed: 8,
		Position:      ActiveTerrain.GridToWorld([2]int{1, 3}),
		PendingAction: &Action{Type: ActionMoveTo, TargetPosition: &highTarget},
	}
	// TargetPosition values below grid dimensions are interpreted as grid
	// coordinates by normalizeActionTargetPosition.

	bots := map[string]*BotState{low.BotID: low, high.BotID: high}
	grid := NewSpatialGrid(20)
	for tick := 0; tick < 10; tick++ {
		ProcessMovement(bots, nil, grid, nil, 0.1)
	}

	lowCell := ActiveTerrain.WorldToGrid(low.Position)
	highCell := ActiveTerrain.WorldToGrid(high.Position)
	if highCell[0] <= lowCell[0] {
		t.Fatalf("MOVE_TO ignored speed: high at %v, low at %v", highCell, lowCell)
	}
}

func TestEstimateBotVelocityUsesTerrainSpeedRatioAndBoost(t *testing.T) {
	setMovementTestConfig(t)
	oldTerrain := ActiveTerrain
	oldCellSize := config.C.PathfindingCellSize
	oldTickRate := config.C.TickRate
	ActiveTerrain = openMovementTerrain(20, 8)
	config.C.PathfindingCellSize = 20
	config.C.TickRate = 10
	t.Cleanup(func() {
		ActiveTerrain = oldTerrain
		config.C.PathfindingCellSize = oldCellSize
		config.C.TickRate = oldTickRate
	})

	tests := []struct {
		name     string
		speed    float64
		effects  []Effect
		expected float64
	}{
		{name: "low", speed: 3.5, expected: 20 * 10 * 0.5 * 3.5 / 5.5},
		{name: "reference", speed: 5.5, expected: 100},
		{name: "high", speed: 8, expected: 20 * 10 * 0.5 * 8 / 5.5},
		{name: "boosted reference", speed: 5.5, effects: []Effect{{Name: "speed_boost", Value: 2}}, expected: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot := &BotState{
				Speed:         tt.speed,
				ActiveEffects: tt.effects,
				PendingAction: &Action{Type: ActionMove, Direction: Vec2{1, 0}},
			}
			velocity := estimateBotVelocity(bot)
			if math.Abs(velocity.X()-tt.expected) > 1e-9 || velocity.Y() != 0 {
				t.Fatalf("estimateBotVelocity() = %v, want [%v 0]", velocity, tt.expected)
			}
		})
	}
}

func TestMultiCellMovementCannotSkipArmedMine(t *testing.T) {
	setMovementTestConfig(t)
	oldTerrain := ActiveTerrain
	oldMode := ActiveModeRules
	ActiveTerrain = openMovementTerrain(16, 5)
	ActiveModeRules = ModeRulesFor(ModeFFA)
	t.Cleanup(func() {
		ActiveTerrain = oldTerrain
		ActiveModeRules = oldMode
	})

	direction := Vec2{1, 0}
	runner := &BotState{
		BotID: "runner", HP: 100, MaxHP: 100, IsAlive: true,
		Speed:         13,
		MoveProgress:  0.7,
		ActiveEffects: []Effect{{Name: "speed_boost", Value: 2}},
		Position:      ActiveTerrain.GridToWorld([2]int{1, 1}),
		PendingAction: &Action{Type: ActionMove, Direction: direction},
	}
	owner := &BotState{
		BotID: "owner", HP: 100, MaxHP: 100, IsAlive: true, MineCount: 1,
		Position: ActiveTerrain.GridToWorld([2]int{12, 1}),
	}
	bots := map[string]*BotState{runner.BotID: runner, owner.BotID: owner}
	grid := NewSpatialGrid(20)
	grid.Insert(runner.BotID, runner.Position)
	grid.Insert(owner.BotID, owner.Position)

	minePosition := ActiveTerrain.GridToWorld([2]int{2, 1})
	mines := []Landmine{{
		ID: "crossed-mine", OwnerID: owner.BotID, Position: minePosition,
		Damage: 30, BlastRadius: 1, Armed: true,
	}}

	ProcessMovement(bots, nil, grid, nil, 0.1)
	if finalCell := ActiveTerrain.WorldToGrid(runner.Position); finalCell != [2]int{4, 1} {
		t.Fatalf("runner final cell = %v, want [4 1] beyond the mine radius", finalCell)
	}
	if IsInRange(runner.Position, minePosition, 1) {
		t.Fatal("test setup invalid: runner final position is still inside mine radius")
	}

	events := UpdateMines(&mines, bots, 10)
	if len(events) != 1 || len(mines) != 0 {
		t.Fatalf("crossed mine did not detonate: events=%d mines=%d", len(events), len(mines))
	}
	if runner.HP != 70 || runner.LastDamageSource != "landmine" {
		t.Fatalf("crossed mine result: hp=%v source=%q, want 70 and landmine", runner.HP, runner.LastDamageSource)
	}
}
