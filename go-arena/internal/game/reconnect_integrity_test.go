package game

import (
	"errors"
	"sync"
	"testing"

	"arena-server/internal/config"
)

func TestAddBotReconnectPreservesActiveRoundState(t *testing.T) {
	previousMax := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = previousMax })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	existing := &BotState{
		BotID:             "reconnect-bot",
		APIKeyID:          "key-1",
		Name:              "Reconnect Bot",
		Position:          NewVec2(420, 360),
		LastValidPosition: NewVec2(420, 360),
		HP:                37,
		MaxHP:             120,
		Weapon:            "sword",
		Stats:             map[string]int{"hp": 7, "speed": 4, "attack": 5, "defense": 4},
		IsAlive:           true,
		RoundKills:        4,
		RoundDeaths:       1,
		RoundDamageDealt:  275,
		SendChan:          make(chan []byte, 2),
	}
	engine.Bots[existing.BotID] = existing
	engine.Grid.Insert(existing.BotID, existing.Position)

	newSend := make(chan []byte, 2)
	reconnected := &BotState{
		BotID:    existing.BotID,
		APIKeyID: existing.APIKeyID,
		Name:     existing.Name,
		HP:       999,
		MaxHP:    999,
		Weapon:   "staff",
		Stats:    map[string]int{"hp": 1, "speed": 1, "attack": 8, "defense": 10},
		SendChan: newSend,
	}

	if admitted := engine.AddBot(reconnected); !admitted {
		t.Fatal("active reconnect was rejected")
	}
	if got := engine.Bots[existing.BotID]; got != reconnected {
		t.Fatalf("engine retained %p, want replacement session %p", got, reconnected)
	}
	if reconnected.HP != 37 || reconnected.MaxHP != 120 || reconnected.Weapon != "sword" ||
		!reconnected.IsAlive || reconnected.RoundKills != 4 || reconnected.RoundDeaths != 1 ||
		reconnected.RoundDamageDealt != 275 {
		t.Fatalf("reconnect reset active match state: %+v", reconnected)
	}
	if reconnected.SendChan != newSend {
		t.Fatal("reconnect did not retain the new session send channel")
	}
	if pos, ok := engine.Grid.GetPosition(existing.BotID); !ok || pos != existing.Position {
		t.Fatalf("spatial grid lost reconnecting bot: position=%v present=%v", pos, ok)
	}
	if reconnected.Stats["hp"] != 7 || reconnected.Stats["attack"] != 5 {
		t.Fatalf("reconnect changed the locked round loadout: %v", reconnected.Stats)
	}
	select {
	case payload := <-newSend:
		t.Fatalf("engine admission queued transport response %q; handler must send the single authoritative confirmation", payload)
	default:
	}
}

func TestDetachBotSessionPreservesActiveStateForReconnect(t *testing.T) {
	previousGrace := config.C.WSReconnectGraceSecs
	previousTickRate := config.C.TickRate
	config.C.WSReconnectGraceSecs = 10
	config.C.TickRate = 10
	t.Cleanup(func() {
		config.C.WSReconnectGraceSecs = previousGrace
		config.C.TickRate = previousTickRate
	})

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 250
	existing := &BotState{
		BotID: "transient-bot", APIKeyID: "key-1", Name: "Transient Bot",
		HP: 42, MaxHP: 120, IsAlive: true, Weapon: "staff",
		Position: NewVec2(300, 240), LastValidPosition: NewVec2(300, 240),
		PendingAction: &Action{Type: ActionAttack},
		SendChan:      make(chan []byte, 2),
	}
	engine.Bots[existing.BotID] = existing
	engine.Grid.Insert(existing.BotID, existing.Position)

	if preserved := engine.DetachBotSession(existing.BotID, existing); !preserved {
		t.Fatal("active transport loss was not preserved for reconnect")
	}
	if got := engine.Bots[existing.BotID]; got != existing {
		t.Fatalf("detached state = %p, want original %p", got, existing)
	}
	if !existing.ReconnectPending || existing.DisconnectedAtTick != 250 {
		t.Fatalf("disconnect metadata = pending:%v tick:%d", existing.ReconnectPending, existing.DisconnectedAtTick)
	}
	if existing.Conn != nil || existing.SendChan != nil || existing.PendingAction != nil {
		t.Fatalf("detached transport/action was retained: conn=%v send=%v action=%v", existing.Conn, existing.SendChan, existing.PendingAction)
	}
	if got := engine.ConnectedBotCount(); got != 0 {
		t.Fatalf("connected count includes detached session: %d", got)
	}

	newSend := make(chan []byte, 2)
	newTicks := make(chan []byte, 1)
	reconnected := &BotState{
		BotID: existing.BotID, APIKeyID: existing.APIKeyID, Name: existing.Name,
		HP: 999, MaxHP: 999, Weapon: "sword", SendChan: newSend, TickChan: newTicks,
	}
	if admitted := engine.AddBot(reconnected); !admitted {
		t.Fatal("reconnect during grace was rejected")
	}
	if reconnected.HP != 42 || reconnected.MaxHP != 120 || reconnected.Weapon != "staff" || !reconnected.IsAlive {
		t.Fatalf("reconnect did not preserve authoritative state: %+v", reconnected)
	}
	if reconnected.ReconnectPending || reconnected.DisconnectedAtTick != 0 {
		t.Fatalf("reconnect retained detach metadata: pending=%v tick=%d", reconnected.ReconnectPending, reconnected.DisconnectedAtTick)
	}
	if reconnected.SendChan != newSend {
		t.Fatal("reconnect lost the new transport channel")
	}
	if reconnected.TickChan != newTicks {
		t.Fatal("reconnect lost the new replaceable tick channel")
	}
}

func TestDetachedBotDoesNotRunFallbackAI(t *testing.T) {
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	bot := &BotState{
		BotID: "detached", IsAlive: true, HP: 100, MaxHP: 100,
		FallbackBehavior: "aggressive", Position: NewVec2(200, 200),
		SendChan: make(chan []byte, 1),
	}
	engine.Bots[bot.BotID] = bot
	if !engine.DetachBotSession(bot.BotID, bot) {
		t.Fatal("active session was not detached")
	}

	engine.applyFallbacks()

	if bot.PendingAction != nil {
		t.Fatalf("detached bot received fallback action: %+v", bot.PendingAction)
	}
}

func TestDetachedSessionExpiresAfterReconnectGrace(t *testing.T) {
	previousGrace := config.C.WSReconnectGraceSecs
	previousTickRate := config.C.TickRate
	config.C.WSReconnectGraceSecs = 1
	config.C.TickRate = 10
	t.Cleanup(func() {
		config.C.WSReconnectGraceSecs = previousGrace
		config.C.TickRate = previousTickRate
	})

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	bot := &BotState{BotID: "expired", IsAlive: true, SendChan: make(chan []byte, 1)}
	engine.Bots[bot.BotID] = bot
	if !engine.DetachBotSession(bot.BotID, bot) {
		t.Fatal("active session was not detached")
	}

	engine.TickCount = 111
	engine.checkAFK()

	if _, present := engine.Bots[bot.BotID]; present {
		t.Fatal("detached bot survived beyond reconnect grace")
	}
}

func TestAddBotReconnectIsAllowedAtCapacity(t *testing.T) {
	previousMax := config.C.MaxBots
	config.C.MaxBots = 1
	t.Cleanup(func() { config.C.MaxBots = previousMax })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	existing := &BotState{BotID: "only-bot", IsAlive: true, HP: 50, SendChan: make(chan []byte, 1)}
	engine.Bots[existing.BotID] = existing

	reconnected := &BotState{BotID: existing.BotID, SendChan: make(chan []byte, 1)}
	if admitted := engine.AddBot(reconnected); !admitted {
		t.Fatal("same-bot reconnect was rejected by the global capacity check")
	}
	if reconnected.HP != 50 || !reconnected.IsAlive {
		t.Fatalf("capacity reconnect did not preserve state: %+v", reconnected)
	}
}

func TestAddBotReconnectBetweenRoundsUsesFreshValidatedLoadout(t *testing.T) {
	for _, phase := range []RoundPhase{PhaseLobby, PhaseIntermission} {
		t.Run(roundPhaseTestName(phase), func(t *testing.T) {
			engine := NewGameEngine()
			engine.Round.Phase = phase
			existing := &BotState{
				BotID: "bot", Weapon: "sword", Stats: map[string]int{"hp": 8, "speed": 4, "attack": 4, "defense": 4},
				HP: 37, MaxHP: 120, IsAlive: true, Elo: 1234, RoundWinStreak: 3,
				Position: NewVec2(200, 200), SendChan: make(chan []byte, 1),
			}
			engine.Bots[existing.BotID] = existing
			engine.Grid.Insert(existing.BotID, existing.Position)
			replacement := &BotState{
				BotID: "bot", Weapon: "staff", Stats: map[string]int{"hp": 5, "speed": 5, "attack": 6, "defense": 4},
				Elo: 1000, SendChan: make(chan []byte, 1),
			}

			if admitted := engine.AddBot(replacement); !admitted {
				t.Fatal("between-round reconnect was rejected")
			}
			if got := engine.Bots[replacement.BotID]; got != replacement {
				t.Fatalf("engine bot = %p, want fresh session %p", got, replacement)
			}
			if replacement.Weapon != "staff" || replacement.Stats["attack"] != 6 {
				t.Fatalf("fresh loadout was overwritten: weapon=%q stats=%v", replacement.Weapon, replacement.Stats)
			}
			if replacement.IsAlive || replacement.HP != 0 || replacement.MaxHP != 0 {
				t.Fatalf("old combat state leaked between rounds: %+v", replacement)
			}
			if replacement.Elo != 1234 || replacement.RoundWinStreak != 3 {
				t.Fatalf("cross-round progression was lost: elo=%d streak=%d", replacement.Elo, replacement.RoundWinStreak)
			}
			if _, present := engine.Grid.GetPosition(replacement.BotID); present {
				t.Fatal("between-round reconnect retained a live grid position")
			}
		})
	}
}

func roundPhaseTestName(phase RoundPhase) string {
	if phase == PhaseIntermission {
		return "intermission"
	}
	return "lobby"
}

func TestLoadoutConfirmationSnapshotIsEngineLocked(t *testing.T) {
	engine := NewGameEngine()
	bot := &BotState{
		BotID: "bot", Weapon: "staff", Stats: map[string]int{"hp": 5, "speed": 5, "attack": 6, "defense": 4},
		Position: NewVec2(100, 100),
	}
	engine.Bots[bot.BotID] = bot

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			engine.mu.Lock()
			bot.Position = NewVec2(float64(i), float64(i))
			engine.mu.Unlock()
		}
	}()
	for i := 0; i < 500; i++ {
		confirmation, current := engine.BuildLoadoutConfirmationForSession(bot.BotID, bot)
		if !current || confirmation["weapon"] != "staff" {
			t.Fatalf("coherent confirmation unavailable: current=%v payload=%v", current, confirmation)
		}
		position := confirmation["position"].(Vec2)
		if position.X() != position.Y() {
			t.Fatalf("torn confirmation position: %v", position)
		}
	}
	wg.Wait()
}

func TestReplacedSessionCannotSubmitIntoReconnect(t *testing.T) {
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.TickCount = 100
	existing := &BotState{BotID: "reconnect-bot", IsAlive: true, SendChan: make(chan []byte, 1)}
	engine.Bots[existing.BotID] = existing

	reconnected := &BotState{BotID: existing.BotID, SendChan: make(chan []byte, 2)}
	if admitted := engine.AddBot(reconnected); !admitted {
		t.Fatal("reconnect was rejected")
	}

	staleAction := &Action{Type: ActionMove, Direction: NewVec2(1, 0)}
	if err := engine.SubmitBotActionForSession(existing.BotID, existing, 100, staleAction); !errors.Is(err, ErrActionSessionReplaced) {
		t.Fatalf("stale session submission error = %v, want %v", err, ErrActionSessionReplaced)
	}
	if reconnected.PendingAction != nil {
		t.Fatalf("stale session changed replacement action to %+v", reconnected.PendingAction)
	}

	currentAction := &Action{Type: ActionMove, Direction: NewVec2(0, 1)}
	if err := engine.SubmitBotActionForSession(reconnected.BotID, reconnected, 100, currentAction); err != nil {
		t.Fatalf("current session submission failed: %v", err)
	}
	if reconnected.PendingAction != currentAction {
		t.Fatalf("current session action = %+v, want %+v", reconnected.PendingAction, currentAction)
	}
}
