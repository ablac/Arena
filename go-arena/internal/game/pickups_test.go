package game

import (
	"testing"

	"arena-server/internal/config"
)

func TestCheckAutoCollectHealthPack(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.HP = 50
	bot.MaxHP = 100
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p1",
		Type:     PickupHealthPack,
		Position: NewVec2(10, 10), // same cell
		Value:    30,
	}
	pickups := []Pickup{pickup}

	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	if bot.HP != 80 {
		t.Errorf("HP after health pack: %v, want 80", bot.HP)
	}
	if len(pickups) != 0 {
		t.Error("pickup should be consumed")
	}
	if bot.RoundPickups != 1 {
		t.Errorf("RoundPickups=%v, want 1", bot.RoundPickups)
	}
}

func TestCheckAutoCollectHealthPackCap(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.HP = 90
	bot.MaxHP = 100
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p1",
		Type:     PickupHealthPack,
		Position: NewVec2(10, 10),
		Value:    30, // would overheal
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	if bot.HP != 100 {
		t.Errorf("HP should be capped at MaxHP=100, got %v", bot.HP)
	}
}

func TestCheckAutoCollectSpeedBoost(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p2",
		Type:     PickupSpeedBoost,
		Position: NewVec2(10, 10),
		Value:    2.0,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	hasSpeedBoost := false
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			hasSpeedBoost = true
		}
	}
	if !hasSpeedBoost {
		t.Error("expected speed_boost effect after pickup")
	}
}

func TestCheckAutoCollectDamageBoost(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p3",
		Type:     PickupDamageBoost,
		Position: NewVec2(10, 10),
		Value:    1.5,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	hasDmgBoost := false
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "damage_boost" {
			hasDmgBoost = true
		}
	}
	if !hasDmgBoost {
		t.Error("expected damage_boost effect after pickup")
	}
}

func TestCheckAutoCollectShieldBubble(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p4",
		Type:     PickupShieldBubble,
		Position: NewVec2(10, 10),
		Value:    50,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	if bot.ShieldAbsorb != 50 {
		t.Errorf("ShieldAbsorb=%v, want 50", bot.ShieldAbsorb)
	}
}

func TestCheckAutoCollectGravityWell(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p5",
		Type:     PickupGravityWell,
		Position: NewVec2(10, 10),
		Value:    1,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	if bot.GravityWellCharge != 1 {
		t.Errorf("GravityWellCharge=%v, want 1", bot.GravityWellCharge)
	}
}

func TestCheckAutoCollectDeadBot(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.IsAlive = false
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "p1",
		Type:     PickupHealthPack,
		Position: NewVec2(10, 10),
		Value:    30,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	if len(pickups) != 1 {
		t.Error("dead bot should not collect pickups")
	}
}

func TestCollectByAction(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.HP = 50
	bot.MaxHP = 100
	bot.Position = NewVec2(10, 10)

	pickup := Pickup{
		ID:       "item1",
		Type:     PickupHealthPack,
		Position: NewVec2(10, 10), // same cell
		Value:    20,
	}
	pickups := []Pickup{pickup}

	ok := CollectByAction(bot, "item1", &pickups)
	if !ok {
		t.Error("expected CollectByAction to succeed")
	}
	if len(pickups) != 0 {
		t.Error("pickup should be consumed")
	}
}

func TestCollectByActionNotFound(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)
	pickups := []Pickup{}

	ok := CollectByAction(bot, "nonexistent", &pickups)
	if ok {
		t.Error("should not collect nonexistent item")
	}
}

func TestSpeedBoostDoesNotStack(t *testing.T) {
	config.Load()
	ActiveTerrain = makeTestTerrain(50, 50, 20)
	defer func() { ActiveTerrain = nil }()

	bot := newTestBot("b", 100)
	bot.Position = NewVec2(10, 10)
	bot.ActiveEffects = []Effect{{Name: "speed_boost", RemainingTicks: 100, Value: 2.0}}

	pickup := Pickup{
		ID:       "p1",
		Type:     PickupSpeedBoost,
		Position: NewVec2(10, 10),
		Value:    2.0,
	}
	pickups := []Pickup{pickup}
	bots := map[string]*BotState{"b": bot}
	CheckAutoCollect(bots, &pickups)

	// Should not stack — only 1 speed boost
	count := 0
	for _, eff := range bot.ActiveEffects {
		if eff.Name == "speed_boost" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 speed_boost effect, got %d", count)
	}
}
