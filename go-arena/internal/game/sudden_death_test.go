package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestSuddenDeathNew(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	if sd.Active {
		t.Error("new SuddenDeathSystem should not be active")
	}
	if len(sd.VoidTiles) != 0 {
		t.Error("new SuddenDeathSystem should have no void tiles")
	}
}

func TestSuddenDeathClear(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	sd.Active = true
	sd.VoidTiles[[2]int{1, 1}] = true
	sd.Clear()

	if sd.Active {
		t.Error("should not be active after clear")
	}
	if len(sd.VoidTiles) != 0 {
		t.Error("void tiles should be cleared")
	}
}

func TestSuddenDeathIsVoidTile(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	sd.VoidTiles[[2]int{3, 5}] = true

	if !sd.IsVoidTile(3, 5) {
		t.Error("(3,5) should be void tile")
	}
	if sd.IsVoidTile(0, 0) {
		t.Error("(0,0) should not be void tile")
	}
}

func TestSuddenDeathGetAllVoidTiles(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	sd.VoidTiles[[2]int{1, 2}] = true
	sd.VoidTiles[[2]int{3, 4}] = true

	tiles := sd.GetAllVoidTiles()
	if len(tiles) != 2 {
		t.Errorf("expected 2 void tiles, got %d", len(tiles))
	}
}

func TestSuddenDeathCheckActivation(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()

	arena := &ArenaMap{
		ZoneRadius: 180,
		MinRadius:  175,
	}
	sd.CheckActivation(arena)
	// ZoneRadius (180) <= MinRadius + 1 (176) is false, so not active
	if sd.Active {
		t.Error("should not activate when zone is not at minimum")
	}

	// Now shrink zone to near minimum
	arena.ZoneRadius = 175
	sd.CheckActivation(arena)
	if !sd.Active {
		t.Error("should activate when zone is at minimum")
	}
}

func TestSuddenDeathCheckActivationAlreadyActive(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	sd.Active = true
	arena := &ArenaMap{ZoneRadius: 500, MinRadius: 175}
	// Should not deactivate if already active
	sd.CheckActivation(arena)
	if !sd.Active {
		t.Error("should remain active once activated")
	}
}

func TestSuddenDeathUpdateDamagesBotOnVoid(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(20, 20, 20)
	defer func() { ActiveTerrain = nil }()

	sd := NewSuddenDeathSystem()
	sd.Active = true

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(50, 50) // cell (2,2)
	cell := ActiveTerrain.WorldToGrid(bot.Position)
	sd.VoidTiles[cell] = true

	arena := &ArenaMap{
		ZoneCenter: NewVec2(200, 200),
		ZoneRadius: 999,
	}
	bots := map[string]*BotState{"b": bot}

	sd.Update(bots, arena)
	// Bot on void tile should take sudden death damage
	if bot.HP >= 100 {
		t.Errorf("bot should take damage on void tile, HP=%v", bot.HP)
	}
}

func TestSuddenDeathUpdateInactive(t *testing.T) {
	config.Load()
	sd := NewSuddenDeathSystem()
	sd.Active = false

	bot := newTestBot("b", 100)
	bots := map[string]*BotState{"b": bot}
	arena := &ArenaMap{}

	result := sd.Update(bots, arena)
	if result != nil {
		t.Error("inactive sudden death should return nil")
	}
	if bot.HP != 100 {
		t.Error("inactive sudden death should not damage bots")
	}
}
