package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

func TestGetBotLiveAllocationsDoNotScaleWithArenaPopulation(t *testing.T) {
	measure := func(botCount int) float64 {
		engine := newBotLiveTestEngine(botCount)
		handler := GetBotLive(engine)
		request := newBotLiveTestRequest()

		return testing.AllocsPerRun(25, func() {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("bot count %d: status = %d, want 200; body: %s", botCount, recorder.Code, recorder.Body.String())
			}
		})
	}

	oneBotAllocs := measure(1)
	fiveHundredBotAllocs := measure(500)
	if growth := fiveHundredBotAllocs - oneBotAllocs; growth > 10 {
		t.Fatalf("/bot/live allocations grew by %.0f with arena population (1 bot: %.0f, 500 bots: %.0f); phase lookup must not snapshot every bot", growth, oneBotAllocs, fiveHundredBotAllocs)
	}
}

func TestGetBotLivePreservesRoundPhaseLabels(t *testing.T) {
	tests := []struct {
		phase game.RoundPhase
		want  string
	}{
		{phase: game.PhaseLobby, want: "lobby"},
		{phase: game.PhaseActive, want: "active"},
		{phase: game.PhaseIntermission, want: "intermission"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			engine := newBotLiveTestEngine(1)
			engine.Round.Phase = tc.phase
			recorder := httptest.NewRecorder()

			GetBotLive(engine).ServeHTTP(recorder, newBotLiveTestRequest())

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Online bool   `json:"online"`
				Phase  string `json:"phase"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !response.Online || response.Phase != tc.want {
				t.Fatalf("response = %#v, want online phase %q", response, tc.want)
			}
		})
	}
}

func BenchmarkGetBotLive500Bots(b *testing.B) {
	engine := newBotLiveTestEngine(500)
	handler := GetBotLive(engine)
	request := newBotLiveTestRequest()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", recorder.Code)
		}
	}
}

func newBotLiveTestEngine(botCount int) *game.GameEngine {
	engine := game.NewGameEngine()
	engine.Round.Phase = game.PhaseActive
	engine.Bots["bot-live"] = &game.BotState{
		BotID:           "bot-live",
		Name:            "Live Bot",
		HP:              75,
		MaxHP:           100,
		Position:        game.NewVec2(40, 60),
		IsAlive:         true,
		ActionHistory:   []game.ActionType{game.ActionMove, game.ActionAttack},
		ActiveEffects:   []game.Effect{},
		Stats:           map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		RoundShotsFired: 2,
		RoundShotsHit:   1,
	}
	for i := 1; i < botCount; i++ {
		id := fmt.Sprintf("filler-%03d", i)
		engine.Bots[id] = &game.BotState{
			BotID:         id,
			Name:          id,
			HP:            100,
			MaxHP:         100,
			Position:      game.NewVec2(float64(i), float64(i)),
			IsAlive:       true,
			ActiveEffects: []game.Effect{},
			Stats:         map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		}
	}
	return engine
}

func newBotLiveTestRequest() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bot/live", nil)
	return request.WithContext(security.WithBotContext(request.Context(), &db.Bot{
		ID:   "bot-live",
		Name: "Live Bot",
	}))
}
