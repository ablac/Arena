package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestPlaceMine(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(50, 50)

	mines := []Landmine{}
	mine := PlaceMine(bot, &mines, 0)
	if mine == nil {
		t.Fatal("expected mine to be placed")
	}
	if len(mines) != 1 {
		t.Errorf("expected 1 mine, got %d", len(mines))
	}
	if mine.OwnerID != "b" {
		t.Errorf("mine owner=%v, want b", mine.OwnerID)
	}
	if mine.Armed {
		t.Error("mine should not be armed immediately")
	}
	if bot.MineCount != 1 {
		t.Errorf("bot MineCount=%v, want 1", bot.MineCount)
	}
}

func TestPlaceMineMaxReached(t *testing.T) {
	config.Load()
	bot := newTestBot("b", 100)

	mines := []Landmine{}
	// Place max mines
	for i := 0; i < config.C.MineMaxPerBot; i++ {
		PlaceMine(bot, &mines, i)
	}
	// One more should fail
	mine := PlaceMine(bot, &mines, config.C.MineMaxPerBot)
	if mine != nil {
		t.Error("should not place mine beyond max")
	}
}

func TestUpdateMinesArming(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	mine := Landmine{
		ID:      "m1",
		OwnerID: "b",
		Position: NewVec2(50, 50),
		Damage:  40,
		Armed:   false,
		ArmTick: 5,
	}
	mines := []Landmine{mine}
	bots := map[string]*BotState{}

	// Before arm tick — stays unarmed
	UpdateMines(&mines, bots, 4)
	if mines[0].Armed {
		t.Error("mine should not arm before ArmTick")
	}

	// At arm tick — should arm
	UpdateMines(&mines, bots, 5)
	if !mines[0].Armed {
		t.Error("mine should arm at ArmTick")
	}
}

func TestUpdateMinesDetonation(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	owner := newTestBot("owner", 100)
	victim := newTestBot("victim", 100)
	victim.Position = NewVec2(50, 50) // will step on mine

	mine := Landmine{
		ID:       "m1",
		OwnerID:  "owner",
		Position: NewVec2(50, 50),
		Damage:   40,
		Armed:    true,
	}
	mines := []Landmine{mine}
	bots := map[string]*BotState{"owner": owner, "victim": victim}

	detonated := UpdateMines(&mines, bots, 10)
	if len(detonated) != 1 || detonated[0] != "m1" {
		t.Errorf("expected mine m1 to detonate, got %v", detonated)
	}
	if len(mines) != 0 {
		t.Error("detonated mine should be removed")
	}
	if victim.HP != 100-40 {
		t.Errorf("victim HP=%v, want %v", victim.HP, 100-40)
	}
	if owner.RoundDamageDealt != 40 {
		t.Errorf("owner RoundDamageDealt=%v, want 40", owner.RoundDamageDealt)
	}
}

func TestUpdateMinesOwnerNotDamaged(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	owner := newTestBot("owner", 100)
	// Owner stands on their own mine
	owner.Position = NewVec2(50, 50)

	victim := newTestBot("victim", 100)
	victim.Position = NewVec2(50, 50)

	mine := Landmine{
		ID:       "m1",
		OwnerID:  "owner",
		Position: NewVec2(50, 50),
		Damage:   40,
		Armed:    true,
	}
	mines := []Landmine{mine}
	bots := map[string]*BotState{"owner": owner, "victim": victim}

	UpdateMines(&mines, bots, 10)
	// Owner should not take damage from own mine
	if owner.HP != 100 {
		t.Errorf("owner should not be damaged by own mine, HP=%v", owner.HP)
	}
}

func TestUpdateMinesUnarmedNotDetonate(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	victim := newTestBot("victim", 100)
	victim.Position = NewVec2(50, 50)

	mine := Landmine{
		ID:       "m1",
		OwnerID:  "owner",
		Position: NewVec2(50, 50),
		Damage:   40,
		Armed:    false,
		ArmTick:  100, // far future
	}
	mines := []Landmine{mine}
	bots := map[string]*BotState{"victim": victim}

	detonated := UpdateMines(&mines, bots, 1)
	if len(detonated) != 0 {
		t.Error("unarmed mine should not detonate")
	}
	if victim.HP != 100 {
		t.Errorf("victim should not take damage from unarmed mine, HP=%v", victim.HP)
	}
}
