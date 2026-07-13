package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"arena-server/internal/config"
)

// withTauntConfig enables taunts for a test and restores config afterwards.
// Taunt tests mutate the global config, so none of them run in parallel
// (package convention).
func withTauntConfig(t *testing.T) {
	t.Helper()
	oldEnabled, oldCooldown, oldTickRate := config.C.TauntsEnabled, config.C.TauntCooldownSecs, config.C.TickRate
	config.C.TauntsEnabled = true
	config.C.TauntCooldownSecs = 5
	if config.C.TickRate <= 0 {
		config.C.TickRate = 10
	}
	t.Cleanup(func() {
		config.C.TauntsEnabled, config.C.TauntCooldownSecs, config.C.TickRate = oldEnabled, oldCooldown, oldTickRate
	})
}

func newTauntTestEngine(t *testing.T) (*GameEngine, *BotState) {
	t.Helper()
	e := NewGameEngine()
	bot := &BotState{
		BotID:    "bragger-1",
		Name:     "Bragger",
		IsAlive:  true,
		HP:       100,
		MaxHP:    100,
		Position: NewVec2(50, 50),
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		SendChan: make(chan []byte, 8),
	}
	e.Bots[bot.BotID] = bot
	e.Round.Phase = PhaseActive
	e.TickCount = 100
	return e, bot
}

func TestAddTauntBuffersSpectatorEvent(t *testing.T) {
	withTauntConfig(t)
	e, bot := newTauntTestEngine(t)

	if err := e.AddTauntForSession(bot.BotID, bot, "gg"); err != nil {
		t.Fatalf("AddTauntForSession: %v", err)
	}

	if len(e.RecentEvents) != 1 {
		t.Fatalf("RecentEvents has %d events, want 1", len(e.RecentEvents))
	}
	ev := e.RecentEvents[0]
	if ev.Type != "taunt" || ev.OwnerID != bot.BotID || ev.Emote != "gg" {
		t.Fatalf("taunt event = %+v, want type=taunt owner=%s emote=gg", ev, bot.BotID)
	}
	if ev.Text != "GG!" {
		t.Fatalf("taunt text = %q, want the server-side table text %q", ev.Text, "GG!")
	}
	if ev.ID != fmt.Sprintf("taunt:%s:%d", bot.BotID, e.TickCount) {
		t.Fatalf("taunt id = %q, want a unique per-bot-per-tick id", ev.ID)
	}
	if bot.LastTauntTick != e.TickCount {
		t.Fatalf("LastTauntTick = %d, want %d", bot.LastTauntTick, e.TickCount)
	}
}

func TestAddTauntCooldown(t *testing.T) {
	withTauntConfig(t)
	e, bot := newTauntTestEngine(t)

	if err := e.AddTauntForSession(bot.BotID, bot, "gg"); err != nil {
		t.Fatalf("first taunt: %v", err)
	}
	e.TickCount++
	if err := e.AddTauntForSession(bot.BotID, bot, "nice"); !IsTauntDropped(err) {
		t.Fatalf("taunt inside cooldown: err = %v, want silent drop", err)
	}
	if len(e.RecentEvents) != 1 {
		t.Fatalf("cooldown taunt still buffered: %d events", len(e.RecentEvents))
	}

	e.TickCount += int(config.C.TauntCooldownSecs * float64(config.C.TickRate))
	if err := e.AddTauntForSession(bot.BotID, bot, "nice"); err != nil {
		t.Fatalf("taunt after cooldown: %v", err)
	}
	if len(e.RecentEvents) != 2 {
		t.Fatalf("post-cooldown taunt not buffered: %d events", len(e.RecentEvents))
	}
}

func TestAddTauntGates(t *testing.T) {
	withTauntConfig(t)

	t.Run("invalid emote", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		if err := e.AddTauntForSession(bot.BotID, bot, "free text here"); err != ErrTauntInvalidEmote {
			t.Fatalf("err = %v, want ErrTauntInvalidEmote", err)
		}
		if len(e.RecentEvents) != 0 {
			t.Fatal("invalid emote buffered an event")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		config.C.TauntsEnabled = false
		t.Cleanup(func() { config.C.TauntsEnabled = true })
		if err := e.AddTauntForSession(bot.BotID, bot, "gg"); !IsTauntDropped(err) {
			t.Fatalf("err = %v, want silent drop when disabled", err)
		}
		if len(e.RecentEvents) != 0 {
			t.Fatal("disabled taunt buffered an event")
		}
	})

	t.Run("round not active", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		e.Round.Phase = PhaseLobby
		if err := e.AddTauntForSession(bot.BotID, bot, "gg"); !IsTauntDropped(err) {
			t.Fatalf("err = %v, want silent drop in lobby", err)
		}
	})

	t.Run("dead bot", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		bot.IsAlive = false
		if err := e.AddTauntForSession(bot.BotID, bot, "gg"); !IsTauntDropped(err) {
			t.Fatalf("err = %v, want silent drop for a dead bot", err)
		}
	})

	t.Run("replaced session", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		stale := &BotState{BotID: bot.BotID}
		if err := e.AddTauntForSession(bot.BotID, stale, "gg"); !IsTauntDropped(err) {
			t.Fatalf("err = %v, want silent drop for a replaced session", err)
		}
	})

	t.Run("unknown bot", func(t *testing.T) {
		e, _ := newTauntTestEngine(t)
		if err := e.AddTauntForSession("nobody", nil, "gg"); !IsTauntDropped(err) {
			t.Fatalf("err = %v, want silent drop for an unknown bot", err)
		}
	})
}

// TestTauntNeverReachesBotTick is the integrity invariant: a buffered taunt
// must ride the (delayed) spectator broadcast and must NEVER appear in any
// bot-facing tick payload, or taunts become a real-time signaling alphabet
// between bots.
func TestTauntNeverReachesBotTick(t *testing.T) {
	withTauntConfig(t)
	e, bot := newTauntTestEngine(t)

	watcher := &BotState{
		BotID:    "watcher-1",
		Name:     "Watcher",
		IsAlive:  true,
		HP:       100,
		MaxHP:    100,
		Position: NewVec2(52, 52), // inside the taunter's fog radius
		Weapon:   "sword",
		Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		SendChan: make(chan []byte, 8),
	}
	e.Bots[watcher.BotID] = watcher

	if err := e.AddTauntForSession(bot.BotID, bot, "gg"); err != nil {
		t.Fatalf("AddTauntForSession: %v", err)
	}

	// Real bot tick path, with the taunt still buffered in RecentEvents.
	e.mu.Lock()
	e.sendBotTickUpdates()
	e.mu.Unlock()

	// Markers unique to the taunt event: its type/emote/text and the event
	// id prefix. The bot id itself appears legitimately in payloads.
	tauntMarkers := [][]byte{
		[]byte(`"type":"taunt"`),
		[]byte(`"emote"`),
		[]byte("GG!"),
		[]byte("taunt:"),
	}
	for _, b := range []*BotState{bot, watcher} {
		for {
			select {
			case payload := <-b.SendChan:
				for _, marker := range tauntMarkers {
					if bytes.Contains(payload, marker) {
						t.Fatalf("bot %s tick payload leaks the taunt (%s): %s", b.BotID, marker, payload)
					}
				}
				continue
			default:
			}
			break
		}
	}

	// Same buffered taunt reaches the spectator broadcast exactly once.
	spec := &SpectatorConn{
		SendChan: make(chan *SpectatorMessage, 8),
		Done:     make(chan struct{}),
	}
	if !e.TryAddSpectator(spec, 10) {
		t.Fatal("TryAddSpectator refused the test spectator")
	}
	e.mu.Lock()
	e.sendSpectatorUpdate()
	e.mu.Unlock()

	select {
	case message := <-spec.SendChan:
		payload := message.Payload
		var state map[string]interface{}
		if err := json.Unmarshal(payload, &state); err != nil {
			t.Fatalf("decode spectator payload: %v", err)
		}
		events, _ := state["events"].([]interface{})
		found := false
		for _, raw := range events {
			ev, _ := raw.(map[string]interface{})
			if ev["type"] == "taunt" && ev["owner_id"] == bot.BotID && ev["text"] == "GG!" {
				found = true
			}
		}
		if !found {
			t.Fatalf("spectator arena_state carries no taunt event: %s", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for spectator broadcast")
	}

	if len(e.RecentEvents) != 0 {
		t.Fatalf("RecentEvents not drained after broadcast: %d left", len(e.RecentEvents))
	}

	// The undelayed admin/debug snapshot must not carry it either.
	snapshot := e.GetState()
	if len(snapshot.Events) != 0 {
		t.Fatalf("undelayed GetState carries %d events; taunts must only ride the delayed broadcast", len(snapshot.Events))
	}
}

// TestTauntDoesNotConsumeActionBudget locks in the other half of the
// design: a taunt must never touch the action machinery, so taunting and
// acting on the same server tick both succeed in either order.
func TestTauntDoesNotConsumeActionBudget(t *testing.T) {
	withTauntConfig(t)

	idleAction := func() *Action { return &Action{Type: ActionIdle} }

	t.Run("taunt then action", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		if err := e.AddTauntForSession(bot.BotID, bot, "gg"); err != nil {
			t.Fatalf("taunt: %v", err)
		}
		if bot.PendingAction != nil {
			t.Fatal("taunt set PendingAction")
		}
		if bot.HasAcceptedServerTick {
			t.Fatal("taunt consumed the per-server-tick action budget")
		}
		if err := e.SubmitBotActionForSession(bot.BotID, bot, e.TickCount, idleAction()); err != nil {
			t.Fatalf("action after same-tick taunt rejected: %v", err)
		}
	})

	t.Run("action then taunt", func(t *testing.T) {
		e, bot := newTauntTestEngine(t)
		if err := e.SubmitBotActionForSession(bot.BotID, bot, e.TickCount, idleAction()); err != nil {
			t.Fatalf("action: %v", err)
		}
		if err := e.AddTauntForSession(bot.BotID, bot, "gg"); err != nil {
			t.Fatalf("taunt after same-tick action rejected: %v", err)
		}
	})
}

// TestTauntClearedAtRoundStart: a taunt buffered after the previous round's
// final broadcast must not ghost into the next round's first frame.
func TestTauntClearedAtRoundStart(t *testing.T) {
	loadTestConfig(t) // startRound generates terrain, which needs real config
	withTauntConfig(t)
	e, bot := newTauntTestEngine(t)

	if err := e.AddTauntForSession(bot.BotID, bot, "too_easy"); err != nil {
		t.Fatalf("taunt: %v", err)
	}
	if len(e.RecentEvents) != 1 {
		t.Fatalf("taunt not buffered: %d events", len(e.RecentEvents))
	}

	e.mu.Lock()
	e.startRound()
	e.mu.Unlock()

	if len(e.RecentEvents) != 0 {
		t.Fatalf("RecentEvents survived startRound: %d events would ghost into the new round", len(e.RecentEvents))
	}
}

func TestTauntEmoteKeysSorted(t *testing.T) {
	keys := TauntEmoteKeys()
	if len(keys) == 0 {
		t.Fatal("no taunt emotes defined")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("emote keys not sorted/unique at %d: %v", i, keys)
		}
	}
	for _, k := range keys {
		text, ok := TauntText(k)
		if !ok || text == "" {
			t.Fatalf("emote %q has no display text", k)
		}
	}
}
