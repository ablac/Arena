package game

import (
	"testing"

	"arena-server/internal/config"
)

// newSuddenDeathFixture wires a fresh system into the package global the
// damage helpers read, and guarantees cleanup so other tests never see an
// active multiplier.
func newSuddenDeathFixture(t *testing.T) *SuddenDeathSystem {
	t.Helper()
	loadTestConfig(t)
	prev := ActiveSuddenDeath
	sd := NewSuddenDeathSystem()
	ActiveSuddenDeath = sd
	t.Cleanup(func() { ActiveSuddenDeath = prev })
	return sd
}

func TestSuddenDeathActivatesAtMinRadius(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	arena := NewArenaMap()

	arena.ZoneRadius = arena.MinRadius + 50
	if sd.CheckActivation(arena, 100, 3000) {
		t.Fatal("activated with the zone still open and time remaining")
	}

	arena.ZoneRadius = arena.MinRadius + 0.5
	if !sd.CheckActivation(arena, 100, 3000) {
		t.Fatal("did not activate when the zone reached minimum radius")
	}
	if !sd.Active {
		t.Fatal("Active flag not set")
	}
	// Second call reports no re-activation.
	if sd.CheckActivation(arena, 101, 3000) {
		t.Fatal("re-activated while already active")
	}
}

func TestSuddenDeathActivatesOnClockExpiry(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	arena := NewArenaMap()
	arena.ZoneRadius = arena.MinRadius + 200 // zone still open

	if sd.CheckActivation(arena, 2999, 3000) {
		t.Fatal("activated before the round clock expired")
	}
	if !sd.CheckActivation(arena, 3000, 3000) {
		t.Fatal("did not activate when the round clock expired")
	}
}

func TestSuddenDeathDoublesDamage(t *testing.T) {
	sd := newSuddenDeathFixture(t)

	if got := SuddenDeathDamageMultiplier(); got != 1 {
		t.Fatalf("inactive multiplier = %v, want 1", got)
	}

	sd.Active = true
	want := config.C.SuddenDeathDamageMult
	if got := SuddenDeathDamageMultiplier(); got != want {
		t.Fatalf("active multiplier = %v, want %v", got, want)
	}

	attacker := &BotState{BotID: "a", Name: "a", IsAlive: true, HP: 100}
	target := &BotState{BotID: "b", Name: "b", IsAlive: true, HP: 100}
	dealt := ApplyDamage(target, attacker, 10, "sword", 1)
	if dealt != 10*want {
		t.Fatalf("ApplyDamage dealt %v during sudden death, want %v", dealt, 10*want)
	}

	sd.Clear()
	if got := SuddenDeathDamageMultiplier(); got != 1 {
		t.Fatalf("multiplier after Clear() = %v, want 1", got)
	}
	target.HP = 100
	if dealt := ApplyDamage(target, attacker, 10, "sword", 2); dealt != 10 {
		t.Fatalf("ApplyDamage dealt %v after Clear, want 10", dealt)
	}
}

func TestStallTimerTriggersRapidDamage(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	sd.Active = true

	bots := map[string]*BotState{
		"a": {BotID: "a", IsAlive: true, HP: 100},
		"b": {BotID: "b", IsAlive: true, HP: 100},
	}

	stallTicks := int(config.C.SuddenDeathStallSeconds * float64(config.C.TickRate))
	// First call baselines, then the full stall window elapses without combat.
	for i := 0; i <= stallTicks; i++ {
		sd.UpdateStall(bots)
	}
	if !sd.StallActive {
		t.Fatalf("stall damage not active after %d no-combat ticks", stallTicks+1)
	}
	if bots["a"].HP >= 100 || bots["b"].HP >= 100 {
		t.Fatalf("stall damage not applied: hp=%v/%v", bots["a"].HP, bots["b"].HP)
	}

	// Stall damage itself must never reset the timer (it is not
	// attacker-attributed), so it keeps applying on subsequent ticks.
	hpBefore := bots["a"].HP
	sd.UpdateStall(bots)
	if bots["a"].HP >= hpBefore {
		t.Fatal("stall damage stopped applying (self-reset bug)")
	}

	// Real combat damage (attacker-attributed) resets the window.
	bots["a"].RoundDamageDealt += 12
	sd.UpdateStall(bots)
	if sd.StallActive {
		t.Fatal("combat damage did not reset the stall window")
	}
	hpAfterReset := bots["b"].HP
	sd.UpdateStall(bots)
	if bots["b"].HP != hpAfterReset {
		t.Fatal("stall damage applied while the window was reset")
	}
}

func TestStallDamageRamps(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	sd.Active = true

	bots := map[string]*BotState{
		"a": {BotID: "a", IsAlive: true, HP: 10000},
		"b": {BotID: "b", IsAlive: true, HP: 10000},
	}

	stallTicks := int(config.C.SuddenDeathStallSeconds * float64(config.C.TickRate))
	rampTicks := int(config.C.SuddenDeathStallRampSeconds * float64(config.C.TickRate))

	tickDamage := func() float64 {
		before := bots["a"].HP
		sd.UpdateStall(bots)
		return before - bots["a"].HP
	}

	for i := 0; i <= stallTicks; i++ {
		sd.UpdateStall(bots)
	}
	base := tickDamage()
	if base != config.C.SuddenDeathStallDamage {
		t.Fatalf("stall tick dealt %v, want %v", base, config.C.SuddenDeathStallDamage)
	}

	// Advance one full ramp interval: per-tick damage must have grown.
	for i := 0; i < rampTicks; i++ {
		sd.UpdateStall(bots)
	}
	if ramped := tickDamage(); ramped <= base {
		t.Fatalf("stall damage did not ramp: %v after %d more ticks (base %v)", ramped, rampTicks+1, base)
	}
}

func TestShouldEndRoundDefersTimeLimitDuringSuddenDeath(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	loadTestConfig(t)

	bots := map[string]*BotState{
		"a": {BotID: "a", IsAlive: true},
		"b": {BotID: "b", IsAlive: true},
	}
	round := &RoundState{StartTick: 0}
	durationTicks := int(config.C.RoundDuration * float64(config.C.TickRate))
	overtimeTicks := int(config.C.SuddenDeathMaxOvertime * float64(config.C.TickRate))

	// Without sudden death, duration expiry ends the round.
	if !ShouldEndRound(bots, round, durationTicks, nil, sd) {
		t.Fatal("round did not end at the duration limit with sudden death inactive")
	}

	// With sudden death active, the duration no longer ends it...
	sd.Active = true
	if ShouldEndRound(bots, round, durationTicks, nil, sd) {
		t.Fatal("round ended at the duration limit despite active sudden death")
	}
	if ShouldEndRound(bots, round, durationTicks+overtimeTicks-1, nil, sd) {
		t.Fatal("round ended before the overtime cap")
	}
	// ...until the hard overtime cap.
	if !ShouldEndRound(bots, round, durationTicks+overtimeTicks, nil, sd) {
		t.Fatal("round did not end at the overtime cap")
	}

	// Elimination still ends the round during overtime.
	bots["b"].IsAlive = false
	if !ShouldEndRound(bots, round, durationTicks+10, nil, sd) {
		t.Fatal("last-bot-standing did not end the round during overtime")
	}
}

func TestVoidTileDamageNotDoubled(t *testing.T) {
	sd := newSuddenDeathFixture(t)
	loadTestConfig(t)

	prevTerrain := ActiveTerrain
	defer func() { ActiveTerrain = prevTerrain }()
	ActiveTerrain = NewTerrainGrid(2000, 2000, nil, 20, 0)

	arena := NewArenaMap()
	arena.ZoneRadius = arena.MinRadius
	sd.Active = true

	bot := &BotState{BotID: "a", IsAlive: true, HP: 5000}
	cell := ActiveTerrain.WorldToGrid(bot.Position)
	sd.VoidTiles[cell] = true

	bots := map[string]*BotState{"a": bot}
	sd.Update(bots, arena)
	if got := 5000 - bot.HP; got != config.C.SuddenDeathDamage {
		t.Fatalf("void tile dealt %v, want unmultiplied %v", got, config.C.SuddenDeathDamage)
	}
}

// TestStuckDetectionUsesPrevTickCell is the regression test for the
// always-stuck bug: LastValidPosition is synced to the current position by
// every movement path within the same tick, so comparing against it made
// every bot read as "not moved" every tick (spear brace permanently ready,
// moving bots nudged sideways every second).
func TestStuckDetectionUsesPrevTickCell(t *testing.T) {
	loadTestConfig(t)

	bot := &BotState{BotID: "a", IsAlive: true}

	// A bot moving one cell per tick — with LastValidPosition synced to the
	// current position, exactly as the movement code does.
	for i := 0; i < 20; i++ {
		bot.Position = NewVec2(float64(100+i*25), 100)
		bot.LastValidPosition = bot.Position
		cell := [2]int{5 + i, 5}
		updateStuckDetection(bot, cell)
		if bot.StuckTicks != 0 || bot.StillTicks != 0 {
			t.Fatalf("moving bot counted as stuck at tick %d: stuck=%d still=%d", i, bot.StuckTicks, bot.StillTicks)
		}
	}

	// A bot standing still accumulates.
	for i := 1; i <= 5; i++ {
		updateStuckDetection(bot, bot.PrevTickCell)
		if bot.StillTicks != i {
			t.Fatalf("stationary bot StillTicks = %d after %d ticks", bot.StillTicks, i)
		}
	}

	// Moving again resets.
	updateStuckDetection(bot, [2]int{50, 50})
	if bot.StuckTicks != 0 || bot.StillTicks != 0 {
		t.Fatal("counters did not reset when the bot moved")
	}
}
