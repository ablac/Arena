package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestProcessTeleportsBasic(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	padA := TeleportPad{
		ID:          "padA",
		Position:    NewVec2(10, 10),
		LinkedPadID: "padB",
		Color:       "#00ffff",
	}
	padB := TeleportPad{
		ID:          "padB",
		Position:    NewVec2(490, 490),
		LinkedPadID: "padA",
		Color:       "#00ffff",
	}
	pads := []TeleportPad{padA, padB}

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10) // on padA
	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	ProcessTeleports(bots, pads, grid, 1)

	if bot.Position != padB.Position {
		t.Errorf("bot should teleport to padB position, got %v", bot.Position)
	}
}

func TestProcessTeleportsCooldown(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	padA := TeleportPad{ID: "padA", Position: NewVec2(10, 10), LinkedPadID: "padB"}
	padB := TeleportPad{ID: "padB", Position: NewVec2(490, 490), LinkedPadID: "padA"}
	pads := []TeleportPad{padA, padB}

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)
	// Pre-set cooldown
	bot.TeleportCooldowns = map[string]int{
		"padA": 100, // cooldown expires at tick 100
	}
	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	// Try at tick 50 — should NOT teleport (cooldown active)
	ProcessTeleports(bots, pads, grid, 50)
	if bot.Position != (NewVec2(10, 10)) {
		t.Errorf("bot should not teleport during cooldown, got %v", bot.Position)
	}

	// Try at tick 101 — should teleport
	bot.Position = NewVec2(10, 10)
	grid.Update("b", bot.Position)
	ProcessTeleports(bots, pads, grid, 101)
	if bot.Position != padB.Position {
		t.Errorf("bot should teleport after cooldown, got %v", bot.Position)
	}
}

func TestProcessTeleportsEmpty(t *testing.T) {
	config.Load()
	// No pads — should not crash
	bot := newTestBot("b", 100)
	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	ProcessTeleports(bots, []TeleportPad{}, grid, 1)
}

func TestProcessTeleportsDeadBot(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	padA := TeleportPad{ID: "padA", Position: NewVec2(10, 10), LinkedPadID: "padB"}
	padB := TeleportPad{ID: "padB", Position: NewVec2(490, 490), LinkedPadID: "padA"}
	pads := []TeleportPad{padA, padB}

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)
	bot.IsAlive = false // dead
	bots := map[string]*BotState{"b": bot}
	grid := NewSpatialGrid(100)
	grid.Insert("b", bot.Position)

	ProcessTeleports(bots, pads, grid, 1)
	// Dead bot should not teleport
	if bot.Position != (NewVec2(10, 10)) {
		t.Errorf("dead bot should not teleport, got %v", bot.Position)
	}
}

func TestBuildTeleportPadView(t *testing.T) {
	config.Load()
	pad := TeleportPad{
		ID:          "padA",
		Position:    NewVec2(100, 200),
		LinkedPadID: "padB",
		Color:       "#ff0000",
	}
	view := BuildTeleportPadView(pad, false)
	if view["id"] != "padA" {
		t.Errorf("id=%v", view["id"])
	}
	if view["linked_pad_id"] != "padB" {
		t.Errorf("linked_pad_id=%v", view["linked_pad_id"])
	}
	if view["color"] != "#ff0000" {
		t.Errorf("color=%v", view["color"])
	}
}
