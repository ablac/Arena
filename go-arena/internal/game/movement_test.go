package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestSeparateBots(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	// Place both bots in the same cell (cell 1,1: world 20-40, 20-40 → center 30,30)
	bot1 := newTestBot("b1", 100)
	bot1.Position = NewVec2(30, 30)
	bot2 := newTestBot("b2", 100)
	bot2.Position = NewVec2(30, 30) // same cell

	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	grid := NewSpatialGrid(100)
	grid.Insert("b1", bot1.Position)
	grid.Insert("b2", bot2.Position)

	SeparateBots(bots, []Obstacle{}, grid)

	// After separation, they should be in different cells
	cell1 := ActiveTerrain.WorldToGrid(bot1.Position)
	cell2 := ActiveTerrain.WorldToGrid(bot2.Position)
	if cell1 == cell2 {
		t.Errorf("bots still in same cell after separation: %v", cell1)
	}
}

func TestSeparateBotsSingleBot(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(30, 30)
	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	origPos := bot.Position
	SeparateBots(bots, []Obstacle{}, grid)
	// Single bot should not move
	if bot.Position != origPos {
		t.Errorf("single bot position changed: %v", bot.Position)
	}
}

func TestSeparateBotsEmpty(t *testing.T) {
	config.Load()
	bots := map[string]*BotState{}
	grid := NewSpatialGrid(100)
	// Should not crash (nil ActiveTerrain → early return)
	SeparateBots(bots, nil, grid)
}

func TestSeparateBotsNoTerrain(t *testing.T) {
	config.Load()
	ActiveTerrain = nil

	bot1 := newTestBot("b1", 100)
	bot1.Position = NewVec2(100, 100)
	bot2 := newTestBot("b2", 100)
	bot2.Position = NewVec2(100, 100)
	bots := map[string]*BotState{"b1": bot1, "b2": bot2}
	grid := NewSpatialGrid(100)

	// Should not panic
	SeparateBots(bots, nil, grid)
}

func TestProcessMovementMoveAction(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	navGrid := NewNavGrid(1000, 1000, nil, 5)
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(500, 500)
	target := NewVec2(600, 500)
	bot.PendingAction = &Action{
		Type:           ActionMove,
		TargetPosition: &target,
	}

	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	ProcessMovement(bots, []Obstacle{}, grid, navGrid, 0.1)

	// Should not panic — movement may or may not happen in one tick
	_ = bot.Position
}

func TestProcessMovementDeadBotDoesNotMove(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	navGrid := NewNavGrid(1000, 1000, nil, 5)
	bot := newTestBot("b", 100)
	bot.IsAlive = false
	bot.Position = NewVec2(500, 500)
	target := NewVec2(600, 500)
	bot.PendingAction = &Action{
		Type:           ActionMove,
		TargetPosition: &target,
	}
	originalPos := bot.Position

	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)

	ProcessMovement(bots, []Obstacle{}, grid, navGrid, 0.1)

	if bot.Position != originalPos {
		t.Errorf("dead bot should not move, got %v", bot.Position)
	}
}

func TestProcessMovementNoAction(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	navGrid := NewNavGrid(1000, 1000, nil, 5)
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(500, 500)
	bot.PendingAction = nil
	originalPos := bot.Position

	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	ProcessMovement(bots, []Obstacle{}, grid, navGrid, 0.1)

	if bot.Position != originalPos {
		t.Errorf("bot without action should not move, got %v", bot.Position)
	}
}

func TestProcessMovementDodge(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	navGrid := NewNavGrid(1000, 1000, nil, 5)
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(500, 500)
	bot.DodgeCooldown = 0
	bot.PendingAction = &Action{
		Type:      ActionDodge,
		Direction: NewVec2(1, 0),
	}

	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	// Should not panic
	ProcessMovement(bots, []Obstacle{}, grid, navGrid, 0.1)
}

func TestProcessMovementStunned(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	navGrid := NewNavGrid(1000, 1000, nil, 5)
	bot := newTestBot("b", 100)
	bot.Position = NewVec2(500, 500)
	bot.StunTicks = 5 // stunned
	target := NewVec2(600, 500)
	bot.PendingAction = &Action{
		Type:           ActionMove,
		TargetPosition: &target,
	}
	originalPos := bot.Position

	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	ProcessMovement(bots, []Obstacle{}, grid, navGrid, 0.1)

	if bot.Position != originalPos {
		t.Errorf("stunned bot should not move, got %v", bot.Position)
	}
}
