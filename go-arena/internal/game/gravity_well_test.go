package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestCreateGravityWell(t *testing.T) {
	config.Load()
	well := CreateGravityWell("owner1", NewVec2(100, 100))
	if well == nil {
		t.Fatal("expected non-nil well")
	}
	if well.OwnerID != "owner1" {
		t.Errorf("OwnerID=%v", well.OwnerID)
	}
	if well.RemainingTicks != config.C.GravityWellDurationTicks {
		t.Errorf("RemainingTicks=%v", well.RemainingTicks)
	}
	if well.ID == "" {
		t.Error("well should have non-empty ID")
	}
}

func TestUpdateGravityWellsExpiry(t *testing.T) {
	config.Load()
	well := GravityWell{
		ID:             "w1",
		OwnerID:        "owner",
		Position:       NewVec2(100, 100),
		RemainingTicks: 1,
		PullRadius:     3,
	}
	wells := []GravityWell{well}
	bots := map[string]*BotState{}
	grid := NewSpatialGrid(100)

	UpdateGravityWells(&wells, bots, grid)
	if len(wells) != 0 {
		t.Error("expired well should be removed")
	}
}

func TestUpdateGravityWellsPulls(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(20, 20, 20)
	defer func() { ActiveTerrain = nil }()

	// Place bot 2 cells away from well center
	victim := newTestBot("victim", 100)
	victim.Position = NewVec2(30, 200) // cell (1,9) approx

	grid := NewSpatialGrid(100)
	grid.Insert("victim", victim.Position)

	wellPos := NewVec2(70, 200) // same row, a few cells right
	well := GravityWell{
		ID:             "w1",
		OwnerID:        "owner",
		Position:       wellPos,
		RemainingTicks: 10,
		PullRadius:     5,
		PullForce:      1.0,
	}
	wells := []GravityWell{well}
	bots := map[string]*BotState{"victim": victim}

	oldX := victim.Position.X()
	UpdateGravityWells(&wells, bots, grid)

	// Victim should have moved toward well (x should increase)
	if victim.Position.X() <= oldX {
		t.Errorf("victim should be pulled toward well: before=%v after=%v", oldX, victim.Position.X())
	}
}

func TestUpdateGravityWellsOwnerNotPulled(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(20, 20, 20)
	defer func() { ActiveTerrain = nil }()

	owner := newTestBot("owner", 100)
	owner.Position = NewVec2(50, 200)

	grid := NewSpatialGrid(100)
	grid.Insert("owner", owner.Position)

	well := GravityWell{
		ID:             "w1",
		OwnerID:        "owner",
		Position:       NewVec2(150, 200),
		RemainingTicks: 5,
		PullRadius:     10,
	}
	wells := []GravityWell{well}
	bots := map[string]*BotState{"owner": owner}

	origPos := owner.Position
	UpdateGravityWells(&wells, bots, grid)

	if owner.Position != origPos {
		t.Errorf("owner should not be pulled by own well")
	}
}

func TestBuildGravityWellView(t *testing.T) {
	config.Load()
	well := GravityWell{
		ID:             "w1",
		OwnerID:        "owner",
		Position:       NewVec2(100, 100),
		RemainingTicks: 10,
		PullRadius:     3,
	}
	view := BuildGravityWellView(well, false)
	if view["id"] != "w1" {
		t.Errorf("view id=%v", view["id"])
	}
	if view["owner_id"] != "owner" {
		t.Errorf("view owner_id=%v", view["owner_id"])
	}
}
