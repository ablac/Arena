package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestSpawnBotAt(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.HP = 10
	bot.IsAlive = false
	bot.StunTicks = 5
	bot.InvulnTicks = 3
	bot.ShieldAbsorb = 20
	bot.ActiveEffects = []Effect{{Name: "speed_boost", RemainingTicks: 10}}
	bot.CooldownRemaining = 1.5
	bot.DodgeCooldown = 3

	grid := NewSpatialGrid(100)
	SpawnBotAt(bot, NewVec2(100, 100), grid, 50)

	if !bot.IsAlive {
		t.Error("bot should be alive after spawn")
	}
	if bot.HP != bot.MaxHP {
		t.Errorf("HP=%v, want MaxHP=%v", bot.HP, bot.MaxHP)
	}
	if bot.StunTicks != 0 {
		t.Errorf("StunTicks=%v, want 0", bot.StunTicks)
	}
	if bot.InvulnTicks != 0 {
		t.Errorf("InvulnTicks=%v, want 0", bot.InvulnTicks)
	}
	if bot.ShieldAbsorb != 0 {
		t.Errorf("ShieldAbsorb=%v, want 0", bot.ShieldAbsorb)
	}
	if bot.CooldownRemaining != 0 {
		t.Errorf("CooldownRemaining=%v, want 0", bot.CooldownRemaining)
	}
	if bot.DodgeCooldown != 0 {
		t.Errorf("DodgeCooldown=%v, want 0", bot.DodgeCooldown)
	}
	if len(bot.ActiveEffects) != 0 {
		t.Errorf("ActiveEffects should be nil, got %v", bot.ActiveEffects)
	}
	if bot.RoundLifeStartTick != 50 {
		t.Errorf("RoundLifeStartTick=%v, want 50", bot.RoundLifeStartTick)
	}
	if bot.GrappleCharges != 2 {
		t.Errorf("GrappleCharges=%v, want 2", bot.GrappleCharges)
	}

	// Check in spatial grid
	pos, ok := grid.GetPosition("b")
	if !ok {
		t.Error("bot not in spatial grid after spawn")
	}
	if pos != bot.Position {
		t.Errorf("grid pos=%v, bot pos=%v", pos, bot.Position)
	}
}

func TestSpawnBotAtBlockedCellSpiralSearch(t *testing.T) {
	config.Load()
	// Create terrain where center (1,1) is blocked, but (0,1) is open
	obs := []Obstacle{{X: 20, Y: 20, Width: 20, Height: 20}} // blocks cell (1,1)
	tg := NewTerrainGrid(200, 200, obs, 20, 0)
	ActiveTerrain = tg
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	grid := NewSpatialGrid(100)

	// Spawn at (30,30) which is inside the obstacle
	SpawnBotAt(bot, NewVec2(30, 30), grid, 0)

	// Bot should be at an unblocked cell
	cell := ActiveTerrain.WorldToGrid(bot.Position)
	if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
		t.Errorf("bot spawned in blocked cell (%v,%v)", cell[0], cell[1])
	}
}

func TestSpawnBotAtNoTerrain(t *testing.T) {
	config.Load()
	ActiveTerrain = nil

	bot := newTestBot("b", 100)
	grid := NewSpatialGrid(100)

	target := NewVec2(300, 400)
	SpawnBotAt(bot, target, grid, 0)

	if bot.Position != target {
		t.Errorf("without terrain, should spawn at exact position %v, got %v", target, bot.Position)
	}
}

func TestCheckDeathsKills(t *testing.T) {
	config.Load()
	bot := newTestBot("victim", 100)
	bot.HP = 0 // dead
	bot.LastDamagedBy = "killer"
	bot.RoundKills = 3

	bots := map[string]*BotState{"victim": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("victim", bot.Position)

	events := CheckDeaths(bots, grid)
	if len(events) != 1 {
		t.Fatalf("expected 1 death event, got %d", len(events))
	}
	ev := events[0]
	if ev.VictimID != "victim" {
		t.Errorf("VictimID=%v", ev.VictimID)
	}
	if ev.KillerID != "killer" {
		t.Errorf("KillerID=%v", ev.KillerID)
	}
	if ev.VictimKills != 3 {
		t.Errorf("VictimKills=%v", ev.VictimKills)
	}

	if bot.IsAlive {
		t.Error("bot should be marked dead")
	}
	if bot.RoundDeaths != 1 {
		t.Errorf("RoundDeaths=%v, want 1", bot.RoundDeaths)
	}

	// Should be removed from spatial grid
	_, ok := grid.GetPosition("victim")
	if ok {
		t.Error("dead bot should be removed from grid")
	}
}

func TestCheckDeathsAliveBot(t *testing.T) {
	config.Load()
	bot := newTestBot("alive", 100)
	bot.HP = 50 // still alive
	bots := map[string]*BotState{"alive": bot}
	grid := NewSpatialGrid(100)

	events := CheckDeaths(bots, grid)
	if len(events) != 0 {
		t.Errorf("alive bot should not produce death event, got %d events", len(events))
	}
	if !bot.IsAlive {
		t.Error("alive bot should remain alive")
	}
}

func TestCheckDeathsAlreadyDead(t *testing.T) {
	config.Load()
	bot := newTestBot("dead", 100)
	bot.HP = 0
	bot.IsAlive = false // already dead
	bots := map[string]*BotState{"dead": bot}
	grid := NewSpatialGrid(100)

	events := CheckDeaths(bots, grid)
	if len(events) != 0 {
		t.Errorf("already-dead bot should not produce new death event, got %d events", len(events))
	}
}

func TestCheckDeathsEmpty(t *testing.T) {
	config.Load()
	bots := map[string]*BotState{}
	grid := NewSpatialGrid(100)
	events := CheckDeaths(bots, grid)
	if len(events) != 0 {
		t.Errorf("no bots should produce no events, got %d", len(events))
	}
}

func TestCheckDeathsMultipleBots(t *testing.T) {
	config.Load()
	b1 := newTestBot("b1", 100)
	b1.HP = 0
	b2 := newTestBot("b2", 100)
	b2.HP = 0
	b3 := newTestBot("b3", 100)
	b3.HP = 50

	bots := map[string]*BotState{"b1": b1, "b2": b2, "b3": b3}
	grid := NewSpatialGrid(100)

	events := CheckDeaths(bots, grid)
	if len(events) != 2 {
		t.Errorf("expected 2 death events, got %d", len(events))
	}
}
