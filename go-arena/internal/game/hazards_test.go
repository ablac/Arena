package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestUpdateHazardsToggle(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	zone := HazardZone{
		ID:            "h1",
		Position:      NewVec2(100, 100),
		Width:         2,
		Height:        2,
		DamagePerTick: 3,
		Active:        true,
		PulseOnTicks:  3,
		PulseOffTicks: 2,
		TickCounter:   0,
	}
	zones := []HazardZone{zone}
	bots := map[string]*BotState{}

	// Tick 1,2: still active (counter=1,2)
	UpdateHazards(zones, bots, 1)
	if !zones[0].Active {
		t.Error("should still be active")
	}
	UpdateHazards(zones, bots, 2)
	if !zones[0].Active {
		t.Error("should still be active at tick 2")
	}
	// Tick 3: counter hits PulseOnTicks=3, toggles off
	UpdateHazards(zones, bots, 3)
	if zones[0].Active {
		t.Error("should toggle off at PulseOnTicks")
	}
	if zones[0].TickCounter != 0 {
		t.Error("counter should reset to 0 after toggle")
	}
}

func TestUpdateHazardsDamageInZone(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(100, 100) // in zone center

	zone := HazardZone{
		ID:            "h1",
		Position:      NewVec2(100, 100),
		Width:         4,
		Height:        4,
		DamagePerTick: 3,
		Active:        true,
		PulseOnTicks:  100,
		PulseOffTicks: 10,
		TickCounter:   0,
	}
	zones := []HazardZone{zone}
	bots := map[string]*BotState{"b": bot}

	UpdateHazards(zones, bots, 1)
	if bot.HP != 97 {
		t.Errorf("bot HP=%v, want 97 (damage=3)", bot.HP)
	}
	if bot.RoundDamageTaken != 3 {
		t.Errorf("RoundDamageTaken=%v, want 3", bot.RoundDamageTaken)
	}
}

func TestUpdateHazardsNoDamageInactive(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(100, 100)

	zone := HazardZone{
		ID:            "h1",
		Position:      NewVec2(100, 100),
		Width:         4,
		Height:        4,
		DamagePerTick: 5,
		Active:        false, // inactive
		PulseOnTicks:  10,
		PulseOffTicks: 5,
		TickCounter:   0,
	}
	zones := []HazardZone{zone}
	bots := map[string]*BotState{"b": bot}

	UpdateHazards(zones, bots, 1)
	if bot.HP != 100 {
		t.Errorf("inactive hazard should not damage, HP=%v", bot.HP)
	}
}

func TestUpdateHazardsNoDamageOutside(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10) // far from zone

	zone := HazardZone{
		ID:            "h1",
		Position:      NewVec2(500, 500),
		Width:         2,
		Height:        2,
		DamagePerTick: 5,
		Active:        true,
		PulseOnTicks:  100,
		PulseOffTicks: 10,
	}
	zones := []HazardZone{zone}
	bots := map[string]*BotState{"b": bot}

	UpdateHazards(zones, bots, 1)
	if bot.HP != 100 {
		t.Errorf("bot outside hazard should not take damage, HP=%v", bot.HP)
	}
}

func TestUpdateHazardsDeadBotIgnored(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(100, 100)
	bot.IsAlive = false

	zone := HazardZone{
		ID:            "h1",
		Position:      NewVec2(100, 100),
		Width:         4,
		Height:        4,
		DamagePerTick: 5,
		Active:        true,
		PulseOnTicks:  100,
		PulseOffTicks: 10,
	}
	zones := []HazardZone{zone}
	bots := map[string]*BotState{"b": bot}

	UpdateHazards(zones, bots, 1)
	if bot.HP != 100 {
		t.Errorf("dead bot should not take hazard damage, HP=%v", bot.HP)
	}
}
