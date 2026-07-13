package ws

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"

	"github.com/gorilla/websocket"
)

func TestBotUpgraderBoundsHandshakesAndPoolsWriteBuffers(t *testing.T) {
	if upgrader.HandshakeTimeout <= 0 || upgrader.HandshakeTimeout > 10*time.Second {
		t.Fatalf("HandshakeTimeout = %v, want a positive bound no greater than 10s", upgrader.HandshakeTimeout)
	}
	if upgrader.WriteBufferPool == nil {
		t.Fatal("bot upgrader must share write buffers instead of retaining 64 KiB per connection")
	}
}

func TestQueryAuthenticatedBotVerifiesAPIKeyOnce(t *testing.T) {
	previousVerifier := botAPIKeyVerifier
	var calls atomic.Int32
	botAPIKeyVerifier = func(_ context.Context, fullKey string) (*db.Bot, error) {
		calls.Add(1)
		if fullKey != "arena_test_key" {
			t.Fatalf("verified key = %q", fullKey)
		}
		return &db.Bot{
			ID:              "bot-1",
			APIKeyID:        "key-1",
			Name:            "One Verify Bot",
			AvatarColor:     "#35d4ff",
			DefaultWeapon:   "sword",
			DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			DefaultFallback: "aggressive",
		}, nil
	}
	t.Cleanup(func() { botAPIKeyVerifier = previousVerifier })

	previousConnectRate := config.C.WSConnectRatePerMin
	config.C.WSConnectRatePerMin = 0
	t.Cleanup(func() { config.C.WSConnectRatePerMin = previousConnectRate })

	server := httptest.NewServer(BotHandler(game.NewGameEngine()))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"?key=arena_test_key",
		nil,
	)
	if err != nil {
		t.Fatalf("dial bot handler: %v", err)
	}
	defer conn.Close()

	var connected struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&connected); err != nil {
		t.Fatalf("read connected message: %v", err)
	}
	if connected.Type != "connected" {
		t.Fatalf("first message type = %q, want connected", connected.Type)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("API key verification calls = %d, want 1", got)
	}
}

func TestFinalAPIKeyCheckRunsWhileSessionIsVisibleAndRollsBack(t *testing.T) {
	originalMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = originalMaxBots })

	wantErr := errors.New("database unavailable")
	tests := []struct {
		name   string
		active bool
		err    error
	}{
		{name: "inactive", active: false},
		{name: "check error fails closed", err: wantErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := game.NewGameEngine()
			bot := &game.BotState{BotID: "bot-1", APIKeyID: "key-1", Name: "Revocation Race"}
			checkStarted := make(chan struct{})
			releaseCheck := make(chan struct{})

			type result struct {
				registered bool
				active     bool
				err        error
			}
			resultCh := make(chan result, 1)
			go func() {
				registered, active, err := registerBotWithActiveAPIKeyCheck(
					context.Background(),
					engine,
					bot,
					func(_ context.Context, keyID string) (bool, error) {
						if keyID != "key-1" {
							return false, errors.New("unexpected key id")
						}
						close(checkStarted)
						<-releaseCheck
						return tt.active, tt.err
					},
				)
				resultCh <- result{registered: registered, active: active, err: err}
			}()

			select {
			case <-checkStarted:
			case <-time.After(time.Second):
				t.Fatal("final API key check did not start")
			}
			if !engine.HasBotSessionForKey(bot.BotID, bot.APIKeyID) {
				t.Fatal("API key was checked before the session became visible to KickBot")
			}
			close(releaseCheck)

			got := <-resultCh
			if !got.registered || got.active {
				t.Fatalf("register/check result = (%v, %v, %v), want registered but rejected", got.registered, got.active, got.err)
			}
			if !errors.Is(got.err, tt.err) {
				t.Fatalf("register/check error = %v, want %v", got.err, tt.err)
			}
			if engine.HasBotSessionForKey(bot.BotID, bot.APIKeyID) {
				t.Fatal("inactive or indeterminate API key left its admitted session registered")
			}
		})
	}
}

func TestFinalAPIKeyRollbackDoesNotRemoveConcurrentReconnect(t *testing.T) {
	originalMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = originalMaxBots })

	engine := game.NewGameEngine()
	checked := make(chan struct{})
	release := make(chan struct{})
	original := &game.BotState{BotID: "bot-1", APIKeyID: "key-1", Name: "Original"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = registerBotWithActiveAPIKeyCheck(context.Background(), engine, original, func(context.Context, string) (bool, error) {
			close(checked)
			<-release
			return false, nil
		})
	}()

	<-checked
	replacement := &game.BotState{BotID: "bot-1", APIKeyID: "key-1", Name: "Replacement"}
	if !engine.AddBot(replacement) {
		t.Fatal("concurrent reconnect was not registered")
	}
	close(release)
	<-done
	if !engine.HasBotSessionForKey(replacement.BotID, replacement.APIKeyID) {
		t.Fatal("rollback of the stale admission removed the newer exact session")
	}
	engine.RemoveBot(replacement.BotID, replacement)
}

func TestBotHandlerClassifiesFinalInactiveKeyAsAuthFailure(t *testing.T) {
	originalVerifier := botAPIKeyVerifier
	originalActiveChecker := botAPIKeyActiveChecker
	originalMetrics := defaultWebSocketAdmissionMetrics
	originalConfig := config.C
	defaultWebSocketAdmissionMetrics = newWebSocketAdmissionMetrics(time.Now)
	config.C.MaxBots = 10
	config.C.WSConnectRatePerMin = 0
	config.C.WSMessageMaxBytes = 1024
	config.C.LoadoutTimeoutSecs = 1
	config.C.StatMin = 1
	config.C.StatMax = 10
	config.C.StatBudget = 20
	botAPIKeyVerifier = func(context.Context, string) (*db.Bot, error) {
		return &db.Bot{
			ID:              "bot-final-check",
			APIKeyID:        "key-final-check",
			Name:            "Final Check",
			AvatarColor:     "#35d4ff",
			DefaultWeapon:   "sword",
			DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			DefaultFallback: "aggressive",
		}, nil
	}
	engine := game.NewGameEngine()
	var visibleDuringCheck atomic.Bool
	botAPIKeyActiveChecker = func(_ context.Context, keyID string) (bool, error) {
		visibleDuringCheck.Store(engine.HasBotSessionForKey("bot-final-check", keyID))
		return false, nil
	}
	t.Cleanup(func() {
		botAPIKeyVerifier = originalVerifier
		botAPIKeyActiveChecker = originalActiveChecker
		defaultWebSocketAdmissionMetrics = originalMetrics
		config.C = originalConfig
	})

	server := httptest.NewServer(BotHandler(engine))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"?key=arena_final_check",
		nil,
	)
	if err != nil {
		t.Fatalf("dial bot handler: %v", err)
	}
	defer conn.Close()

	var connected struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&connected); err != nil || connected.Type != "connected" {
		t.Fatalf("connected message = (%+v, %v)", connected, err)
	}
	if err := conn.WriteJSON(map[string]interface{}{
		"type":              "select_loadout",
		"weapon":            "sword",
		"stats":             map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		"fallback_behavior": "aggressive",
	}); err != nil {
		t.Fatalf("send loadout: %v", err)
	}
	var rejection struct {
		Code string `json:"code"`
	}
	if err := conn.ReadJSON(&rejection); err != nil {
		t.Fatalf("read final key rejection: %v", err)
	}
	if rejection.Code != "API_KEY_REVOKED" {
		t.Fatalf("rejection code = %q, want API_KEY_REVOKED", rejection.Code)
	}
	if !visibleDuringCheck.Load() {
		t.Fatal("handler ran final API key check before registering the session")
	}
	if engine.HasBotSessionForKey("bot-final-check", "key-final-check") {
		t.Fatal("handler retained the inactive-key session")
	}
	metrics := defaultWebSocketAdmissionMetrics.snapshot().Bot.Totals
	if metrics.Attempts != 1 || metrics.Upgrades != 1 || metrics.Admissions != 0 || metrics.Failures.Auth != 1 || metrics.Failures.Capacity != 0 {
		t.Fatalf("bot admission metrics = %#v, want final-check auth failure", metrics)
	}
}
