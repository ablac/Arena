package game

import (
	"encoding/json"
	"testing"

	"arena-server/internal/config"
)

func TestSpawnAndRoundResetClearDamageAttribution(t *testing.T) {
	previousTerrain := ActiveTerrain
	ActiveTerrain = nil
	t.Cleanup(func() { ActiveTerrain = previousTerrain })

	bot := &BotState{
		BotID:            "victim",
		MaxHP:            100,
		LastDamagedBy:    "old-attacker",
		LastDamageTick:   45,
		LastDamageSource: "old-weapon",
		LastDamageAmount: 99,
	}
	SpawnBotAt(bot, NewVec2(2, 3), NewSpatialGrid(10), 50)
	assertDamageAttributionCleared(t, bot)

	bot.LastDamagedBy = "round-attacker"
	bot.LastDamageTick = 75
	bot.LastDamageSource = "round-weapon"
	bot.LastDamageAmount = 25
	bot.ResetRoundStats()
	assertDamageAttributionCleared(t, bot)
}

func assertDamageAttributionCleared(t *testing.T, bot *BotState) {
	t.Helper()
	if bot.LastDamagedBy != "" || bot.LastDamageTick != 0 || bot.LastDamageSource != "" || bot.LastDamageAmount != 0 {
		t.Fatalf("damage attribution not cleared: attacker=%q tick=%d source=%q damage=%v",
			bot.LastDamagedBy, bot.LastDamageTick, bot.LastDamageSource, bot.LastDamageAmount)
	}
}

func TestCheckDeathsPreservesRecentHitSourceAndDamage(t *testing.T) {
	previousTickRate := config.C.TickRate
	previousMode := ActiveModeRules
	config.C.TickRate = 10
	ActiveModeRules = ModeRulesFor(ModeFFA)
	t.Cleanup(func() {
		config.C.TickRate = previousTickRate
		ActiveModeRules = previousMode
	})

	attacker := &BotState{BotID: "attacker", Name: "Attacker", Weapon: "staff", HP: 100, IsAlive: true}
	victim := &BotState{BotID: "victim", Name: "Victim", HP: 100, IsAlive: true, ShieldAbsorb: 3}
	bots := map[string]*BotState{attacker.BotID: attacker, victim.BotID: victim}

	if actual := ApplyDamage(victim, attacker, 10, "staff_burn", 100); actual != 7 {
		t.Fatalf("actual damage = %v, want 7", actual)
	}
	attacker.Weapon = "sword"
	victim.RoundDamageTaken = 999
	victim.HP = 0 // A nearby environmental effect finishes the victim.

	grid := NewSpatialGrid(10)
	grid.Insert(victim.BotID, victim.Position)
	deaths := CheckDeaths(bots, grid, 149)
	if len(deaths) != 1 {
		t.Fatalf("death events = %d, want 1", len(deaths))
	}
	death := deaths[0]
	if death.KillerID != attacker.BotID || death.KillerName != attacker.Name {
		t.Fatalf("killer = (%q, %q), want (%q, %q)", death.KillerID, death.KillerName, attacker.BotID, attacker.Name)
	}
	if death.Weapon != "staff_burn" {
		t.Fatalf("weapon = %q, want last damage source staff_burn", death.Weapon)
	}
	if death.Damage != 7 {
		t.Fatalf("damage = %v, want effective last-hit damage 7", death.Damage)
	}
}

func TestCheckDeathsRejectsStaleEnvironmentalAttribution(t *testing.T) {
	previousTickRate := config.C.TickRate
	config.C.TickRate = 10
	t.Cleanup(func() { config.C.TickRate = previousTickRate })

	attacker := &BotState{BotID: "attacker", HP: 100, IsAlive: true}
	victim := &BotState{
		BotID:            "victim",
		HP:               0,
		IsAlive:          true,
		LastDamagedBy:    attacker.BotID,
		LastDamageTick:   100,
		LastDamageSource: "bow",
		LastDamageAmount: 18,
	}
	bots := map[string]*BotState{attacker.BotID: attacker, victim.BotID: victim}
	deaths := CheckDeaths(bots, NewSpatialGrid(10), 151)
	if len(deaths) != 1 {
		t.Fatalf("death events = %d, want 1", len(deaths))
	}
	death := deaths[0]
	if death.KillerID != "" || death.KillerName != "" || death.Weapon != "" || death.Damage != 0 {
		t.Fatalf("stale hit received kill credit: %+v", death)
	}
}

func TestHandleKillCreditsUsesAttributedHitMetadata(t *testing.T) {
	previousHook := GameEventHook
	t.Cleanup(func() { GameEventHook = previousHook })

	killer := &BotState{BotID: "killer", Name: "Killer", Weapon: "sword", HP: 100, IsAlive: true, Elo: 1000}
	victim := &BotState{BotID: "victim", Name: "Victim", RoundDamageTaken: 999, Elo: 1000}
	engine := &GameEngine{
		Bots:       map[string]*BotState{killer.BotID: killer, victim.BotID: victim},
		KillFeed:   NewKillFeed(10),
		Bounty:     NewBountySystem(),
		ModeRules:  ModeRulesFor(ModeFFA),
		TeamScores: make(map[int]int),
		TickCount:  200,
	}

	var dashboardEvent map[string]interface{}
	GameEventHook = func(eventName string, data map[string]interface{}) {
		if eventName == "kill" {
			dashboardEvent = data
		}
	}
	deaths := []DeathEvent{{
		VictimID:   victim.BotID,
		KillerID:   killer.BotID,
		KillerName: killer.Name,
		Weapon:     "landmine",
		Damage:     30,
	}}
	engine.handleKillCredits(deaths)

	if len(engine.KillEvents) != 1 {
		t.Fatalf("kill events = %d, want 1", len(engine.KillEvents))
	}
	if got := engine.KillEvents[0]; got.Weapon != "landmine" || got.Damage != 30 {
		t.Fatalf("kill event metadata = (%q, %v), want (landmine, 30)", got.Weapon, got.Damage)
	}
	feed := engine.KillFeed.GetAll()
	if len(feed) != 1 || feed[0].Weapon != "landmine" {
		t.Fatalf("kill feed = %+v, want attributed landmine source", feed)
	}
	if dashboardEvent == nil || dashboardEvent["weapon"] != "landmine" || dashboardEvent["damage"] != float64(30) {
		t.Fatalf("dashboard kill metadata = %+v, want attributed source and damage", dashboardEvent)
	}
}

func TestSendKillMessageIncludesAttributedDamage(t *testing.T) {
	bot := &BotState{BotID: "killer", SendChan: make(chan []byte, 1)}
	SendKillMessage(bot, KillEvent{VictimID: "victim", Weapon: "bow", Damage: 12.5})

	var message map[string]interface{}
	if err := json.Unmarshal(<-bot.SendChan, &message); err != nil {
		t.Fatalf("decode kill message: %v", err)
	}
	if message["damage"] != 12.5 {
		t.Fatalf("kill message damage = %v, want 12.5", message["damage"])
	}
}
