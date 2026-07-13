package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"

	"github.com/gorilla/websocket"
)

const testBotFrameLimit = 192

func TestBotReaderKeepsLegacyStaffDualTargetSessionUnlocked(t *testing.T) {
	withWSIntegrityConfig(t)
	tracker := installActionStrikeTestTracker(t)
	engine := game.NewGameEngine()
	engine.Round.Phase = game.PhaseActive
	engine.TickCount = 100

	cfg := config.C
	cfg.HeartbeatInterval = 1
	cfg.WSMaxMessagesPerSec = 25
	readyCh := make(chan *game.BotState, 1)
	resultCh := make(chan *game.BotState, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			resultCh <- nil
			return
		}
		defer conn.Close()
		bot := &game.BotState{
			BotID: "legacy-staff", APIKeyID: "legacy-staff-key", Name: "Legacy Staff",
			Weapon: "staff", IsAlive: true, HP: 100, MaxHP: 100,
			Conn: conn, SendChan: make(chan []byte, 8),
		}
		engine.Bots[bot.BotID] = bot
		readyCh <- bot
		ctx, cancel := context.WithCancel(context.Background())
		botReader(ctx, cancel, conn, bot, engine, &cfg)
		resultCh <- bot
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial legacy staff harness: %v", err)
	}
	bot := <-readyCh
	for attempt := 0; attempt < 3; attempt++ {
		if err := conn.WriteJSON(map[string]interface{}{
			"type": "action", "tick": 100, "action": "attack",
			"target": "opponent-id", "target_position": [2]float64{100, 140},
		}); err != nil {
			_ = conn.Close()
			t.Fatalf("write legacy staff action %d: %v", attempt+1, err)
		}
	}
	responseCodes := make([]string, 0, 2)
	for response := 0; response < 2; response++ {
		select {
		case payload := <-bot.SendChan:
			var message struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(payload, &message); err != nil {
				_ = conn.Close()
				t.Fatalf("decode duplicate response %d: %v", response+1, err)
			}
			responseCodes = append(responseCodes, message.Code)
		case <-time.After(time.Second):
			_ = conn.Close()
			t.Fatalf("legacy staff reader did not process response %d", response+1)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		_ = conn.Close()
		t.Fatal("legacy staff socket produced an unexpected frame")
	} else if netErr, ok := err.(interface{ Timeout() bool }); !ok || !netErr.Timeout() {
		_ = conn.Close()
		t.Fatalf("legacy staff socket was policy-closed after compatibility actions: %v", err)
	}
	for response, code := range responseCodes {
		if code != "DUPLICATE_ACTION_TICK" {
			_ = conn.Close()
			t.Fatalf("legacy staff response %d code = %q, want DUPLICATE_ACTION_TICK", response+1, code)
		}
	}
	if bot.PendingAction == nil || bot.PendingAction.TargetPosition == nil || bot.PendingAction.TargetID != "" {
		_ = conn.Close()
		t.Fatalf("legacy staff action reached engine as %+v", bot.PendingAction)
	}
	if entry := tracker.entries[bot.APIKeyID]; entry != nil {
		_ = conn.Close()
		t.Fatalf("legacy staff compatibility action created protocol strikes: %+v", entry)
	}
	if remaining, locked := tracker.IsLocked(bot.APIKeyID); locked || remaining != 0 {
		_ = conn.Close()
		t.Fatalf("legacy staff key lock state = remaining %v locked %v", remaining, locked)
	}
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second),
	)
	_ = conn.Close()

	var completedBot *game.BotState
	select {
	case completedBot = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy staff reader did not finish")
	}
	if completedBot == nil {
		t.Fatal("legacy staff harness failed to upgrade")
	}
	if completedBot != bot {
		t.Fatalf("legacy staff reader completed a different session: got %p want %p", completedBot, bot)
	}
}

func TestBotReaderReportsPeerCloseDetails(t *testing.T) {
	withWSIntegrityConfig(t)
	engine := game.NewGameEngine()
	engine.Round.Phase = game.PhaseActive

	cfg := config.C
	cfg.HeartbeatInterval = 1
	cfg.WSMaxMessagesPerSec = 25
	resultCh := make(chan botSessionEnd, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		bot := &game.BotState{
			BotID: "peer-close", APIKeyID: "peer-close-key", Name: "Peer Close",
			Weapon: "bow", IsAlive: true, HP: 100, MaxHP: 100,
			Conn: conn, SendChan: make(chan []byte, 1),
		}
		ctx, cancel := context.WithCancel(context.Background())
		resultCh <- botReader(ctx, cancel, conn, bot, engine, &cfg)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial close-details harness: %v", err)
	}
	if err := conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client shutdown"),
		time.Now().Add(time.Second),
	); err != nil {
		_ = conn.Close()
		t.Fatalf("write peer close frame: %v", err)
	}
	_ = conn.Close()

	select {
	case result := <-resultCh:
		if result.Source != "peer" {
			t.Fatalf("disconnect source = %q, want peer", result.Source)
		}
		if result.CloseCode != websocket.CloseNormalClosure {
			t.Fatalf("close code = %d, want %d", result.CloseCode, websocket.CloseNormalClosure)
		}
		if result.CloseReason != "client shutdown" {
			t.Fatalf("close reason = %q, want client shutdown", result.CloseReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bot reader did not report peer close")
	}
}

func TestBotWriterReportsWriteFailure(t *testing.T) {
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
	}))
	defer server.Close()

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial writer-failure harness: %v", err)
	}
	defer clientConn.Close()
	serverConn := <-serverConnCh
	if err := serverConn.Close(); err != nil {
		t.Fatalf("close server side before writer probe: %v", err)
	}

	sendChan := make(chan []byte, 1)
	sendChan <- []byte(`{"type":"tick"}`)
	result := botWriter(context.Background(), serverConn, sendChan, make(chan []byte))

	if result.Source != "server_writer" {
		t.Fatalf("writer source = %q, want server_writer", result.Source)
	}
	if result.Err == nil {
		t.Fatal("writer failure did not retain its transport error")
	}
}

func TestResolveBotSessionEndPrefersEngineCloseCause(t *testing.T) {
	bot := &game.BotState{TransportCloseCause: make(chan game.BotTransportCloseCause, 1)}
	bot.SignalTransportClose(game.BotTransportCloseCause{
		Source: "server_policy", CloseCode: websocket.ClosePolicyViolation, CloseReason: "AFK timeout",
	})

	result := resolveBotSessionEnd(
		bot,
		botSessionEnd{Source: "peer", Err: errors.New("closed network connection"), ActionsReceived: 7},
		botSessionEnd{Source: "server_context"},
	)

	if result.Source != "server_policy" || result.CloseCode != websocket.ClosePolicyViolation || result.CloseReason != "AFK timeout" {
		t.Fatalf("resolved engine cause = %+v", result)
	}
	if result.ActionsReceived != 7 {
		t.Fatalf("resolved action count = %d, want 7", result.ActionsReceived)
	}
}

func TestResolveBotSessionEndKeepsObservedPeerCloseOverWriterError(t *testing.T) {
	bot := &game.BotState{TransportCloseCause: make(chan game.BotTransportCloseCause, 1)}
	result := resolveBotSessionEnd(
		bot,
		botSessionEnd{
			Source: "peer", CloseCode: websocket.CloseNormalClosure,
			CloseReason: "client shutdown", ActionsReceived: 4,
		},
		botSessionEnd{Source: "server_writer", Err: errors.New("write failed after peer close")},
	)

	if result.Source != "peer" || result.CloseCode != websocket.CloseNormalClosure || result.CloseReason != "client shutdown" {
		t.Fatalf("resolved peer close = %+v", result)
	}
}

func TestBotHandlerLimitsOversizedAuthenticationFrameBeforeAuth(t *testing.T) {
	previousLimit := config.C.WSMessageMaxBytes
	previousTimeout := config.C.ConnectionTimeout
	previousConnectRate := config.C.WSConnectRatePerMin
	config.C.WSMessageMaxBytes = testBotFrameLimit
	config.C.ConnectionTimeout = 1
	config.C.WSConnectRatePerMin = 0
	t.Cleanup(func() {
		config.C.WSMessageMaxBytes = previousLimit
		config.C.ConnectionTimeout = previousTimeout
		config.C.WSConnectRatePerMin = previousConnectRate
	})

	server := httptest.NewServer(BotHandler(game.NewGameEngine()))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial bot handler: %v", err)
	}
	defer conn.Close()

	payload := []byte(`{"type":"auth","api_key":"arena_` + strings.Repeat("x", testBotFrameLimit) + `"}`)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write oversized auth frame: %v", err)
	}
	requireWebSocketCloseCode(t, conn, websocket.CloseMessageTooBig)
}

func TestBotUpgradeRateLimitSupportsMessageAuthBotsBehindNAT(t *testing.T) {
	previousRate := config.C.WSConnectRatePerMin
	config.C.WSConnectRatePerMin = 3
	t.Cleanup(func() {
		config.C.WSConnectRatePerMin = previousRate
	})

	limitKey, limit := botUpgradeRateLimit("203.0.113.7")
	if limitKey != "ws:bot:connect:203.0.113.7" {
		t.Fatalf("connect bucket = %q, want one non-bypassable per-IP bucket", limitKey)
	}
	if limit != 40 {
		t.Fatalf("NAT reconnect limit = %d, want bounded burst 40", limit)
	}

	config.C.WSConnectRatePerMin = 75
	_, configuredLimit := botUpgradeRateLimit("203.0.113.7")
	if configuredLimit != 75 {
		t.Fatalf("configured reconnect limit = %d, want 75", configuredLimit)
	}
}

func TestBotKeyConnectRateLimitSeparatesAdmissionFromResume(t *testing.T) {
	key, limit, window := botKeyConnectRateLimit("bot-1", "key-1", false)
	if key != "ws:bot:key:key-1" || limit != 1 || window != 5 {
		t.Fatalf("new admission bucket = %q %d/%ds, want legacy 1/5s", key, limit, window)
	}

	key, limit, window = botKeyConnectRateLimit("bot-1", "key-1", true)
	if key != "ws:bot:resume:bot-1:key-1" || limit != 3 || window != 5 {
		t.Fatalf("resume bucket = %q %d/%ds, want bounded 3/5s recovery burst", key, limit, window)
	}
}

func TestLoadoutPhaseLimitsOversizedSelectionFrame(t *testing.T) {
	cfg := config.C
	cfg.WSMessageMaxBytes = testBotFrameLimit
	cfg.LoadoutTimeoutSecs = 1
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			handlerErr <- err
			return
		}
		defer conn.Close()
		bot := &game.BotState{
			BotID:            "oversized-loadout",
			Name:             "Oversized Loadout",
			Weapon:           "sword",
			Stats:            map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			FallbackBehavior: "aggressive",
		}
		handlerErr <- handleLoadoutPhase(conn, bot, &cfg)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial loadout harness: %v", err)
	}
	defer conn.Close()

	payload := []byte(`{"type":"select_loadout","weapon":"sword","stats":{"hp":5,"speed":5,"attack":5,"defense":5},"fallback_behavior":"aggressive","padding":"` + strings.Repeat("x", testBotFrameLimit) + `"}`)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write oversized loadout frame: %v", err)
	}
	requireWebSocketCloseCode(t, conn, websocket.CloseMessageTooBig)
	if err := <-handlerErr; err == nil {
		t.Fatal("oversized loadout frame did not fail the loadout phase")
	}
}

func TestLoadoutReadTimeoutIsTerminal(t *testing.T) {
	cfg := config.C
	cfg.WSMessageMaxBytes = 1024
	cfg.LoadoutTimeoutSecs = 0.02
	handlerErr := make(chan error, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			handlerErr <- err
			return
		}
		defer conn.Close()
		bot := &game.BotState{
			BotID: "timeout-loadout", Name: "Timeout Loadout", Weapon: "sword",
			Stats:            map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			FallbackBehavior: "aggressive",
		}
		handlerErr <- handleLoadoutPhase(conn, bot, &cfg)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial timeout harness: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-handlerErr:
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("loadout timeout error = %v, want terminal timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("loadout timeout did not terminate negotiation")
	}
}

func TestAdmissionBanRecheckRejectsPermanentBan(t *testing.T) {
	tests := []struct {
		name     string
		ban      func(*game.GameEngine)
		wantCode string
	}{
		{name: "key", ban: func(engine *game.GameEngine) { engine.BanKey("key-1") }, wantCode: "KEY_BANNED"},
		{name: "ip", ban: func(engine *game.GameEngine) { engine.BanIP("203.0.113.10") }, wantCode: "IP_BANNED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := game.NewGameEngine()
			tt.ban(engine)
			result := make(chan bool, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					result <- false
					return
				}
				defer conn.Close()
				result <- rejectPermanentlyBannedBot(engine, conn, "Bot", "bot-1", "203.0.113.10", "key-1")
			}))
			defer server.Close()

			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatalf("dial ban harness: %v", err)
			}
			defer conn.Close()
			var message struct {
				Code string `json:"code"`
			}
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatalf("read ban rejection: %v", err)
			}
			handled := <-result
			if !handled || message.Code != tt.wantCode {
				t.Fatalf("ban rejection = handled %v code %q, want true/%q", handled, message.Code, tt.wantCode)
			}
		})
	}
}

func TestStaleRuntimeSessionProtocolLockDisconnectsReplacement(t *testing.T) {
	tracker := installActionStrikeTestTracker(t)
	previousMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = previousMaxBots })
	engine := game.NewGameEngine()
	ready := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			close(ready)
			return
		}
		defer conn.Close()
		active := &game.BotState{
			BotID: "bot-1", APIKeyID: "key-1", Name: "Active Bot",
			Conn: conn, SendChan: make(chan []byte, 1),
		}
		if !engine.AddBot(active) {
			close(ready)
			return
		}
		close(ready)
		<-release
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		close(release)
		t.Fatalf("dial active-session harness: %v", err)
	}
	defer conn.Close()
	<-ready
	staleSession := &game.BotState{BotID: "bot-1", APIKeyID: "key-1", SendChan: make(chan []byte, 1)}
	tracker.Record(staleSession.APIKeyID)
	tracker.Record(staleSession.APIKeyID)
	if !rejectBotViolation(engine, staleSession, "Invalid message", "INVALID_MESSAGE", nil) {
		close(release)
		t.Fatal("third stale-session strike did not lock the shared key")
	}
	<-staleSession.SendChan // structured lock response was still delivered to the offender
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		close(release)
		t.Fatal("active arena socket stayed open after shared key was locked")
	}
	close(release)
}

func TestAdmissionReceivesOneAuthoritativeLoadoutConfirmation(t *testing.T) {
	withWSIntegrityConfig(t)
	previousMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = previousMaxBots })

	tests := []struct {
		name           string
		setup          func(*game.GameEngine, string)
		expectedWeapon string
	}{
		{
			name:           "new connection confirms selected loadout",
			expectedWeapon: "staff",
		},
		{
			name: "active reconnect confirms preserved loadout",
			setup: func(engine *game.GameEngine, botID string) {
				engine.Round.Phase = game.PhaseActive
				engine.Bots[botID] = &game.BotState{
					BotID:            botID,
					APIKeyID:         "key-1",
					Name:             "Single Confirmation",
					Weapon:           "sword",
					Stats:            map[string]int{"hp": 7, "speed": 4, "attack": 5, "defense": 4},
					FallbackBehavior: "aggressive",
					HP:               72,
					MaxHP:            120,
					IsAlive:          true,
					SendChan:         make(chan []byte, 4),
				}
			},
			expectedWeapon: "sword",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := game.NewGameEngine()
			botID := "single-confirmation-bot"
			if tc.setup != nil {
				tc.setup(engine, botID)
			}
			confirmations, queued := runAdmissionConfirmationHarness(t, engine, botID)
			if queued != 0 {
				t.Fatalf("admission queued %d extra message(s)", queued)
			}
			if len(confirmations) != 1 || confirmations[0].Weapon != tc.expectedWeapon {
				t.Fatalf("admission confirmations = %+v, want one authoritative %s confirmation", confirmations, tc.expectedWeapon)
			}
		})
	}
}

func TestPostAdmissionCosmeticRefreshClosesReversalNegotiationGap(t *testing.T) {
	withWSIntegrityConfig(t)
	previousMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = previousMaxBots })
	engine := game.NewGameEngine()
	bot := &game.BotState{
		BotID: "admission-cosmetic-race",
		Cosmetics: map[string]string{
			"bot_skin": "now_refunded_skin",
		},
		SendChan: make(chan []byte, 1),
		TickChan: make(chan []byte, 1),
	}

	// The reversal snapshot happens while loadout negotiation is in progress,
	// so this connection is not yet visible to ConnectedBotIDs.
	if got := engine.ConnectedBotIDs(); len(got) != 0 {
		t.Fatalf("pre-admission reversal snapshot = %v, want empty", got)
	}
	// The durable reversal commits, then this connection completes admission.
	if !engine.AddBot(bot) {
		t.Fatal("bot admission failed")
	}
	loaderCalls := 0
	current, err := refreshAdmittedBotCosmetics(context.Background(), engine, bot.BotID, func(_ context.Context, botID string) (map[string]string, error) {
		loaderCalls++
		if botID != bot.BotID {
			t.Fatalf("loader bot ID = %q, want %q", botID, bot.BotID)
		}
		return map[string]string{"bot_skin": "standard", "weapon_skin": "standard", "attachment": "none"}, nil
	})
	if err != nil || !current || loaderCalls != 1 {
		t.Fatalf("post-admission refresh = current %t, calls %d, err %v", current, loaderCalls, err)
	}
	if got := engine.Bots[bot.BotID].Cosmetics["bot_skin"]; got != "standard" {
		t.Fatalf("admitted bot retained pre-reversal cosmetic %q", got)
	}
}

func TestPostAdmissionCosmeticReadFailureClearsPotentiallyRevokedVisuals(t *testing.T) {
	withWSIntegrityConfig(t)
	previousMaxBots := config.C.MaxBots
	config.C.MaxBots = 10
	t.Cleanup(func() { config.C.MaxBots = previousMaxBots })
	engine := game.NewGameEngine()
	bot := &game.BotState{
		BotID:     "admission-cosmetic-failure",
		Cosmetics: map[string]string{"bot_skin": "possibly_refunded_skin"},
		SendChan:  make(chan []byte, 1),
		TickChan:  make(chan []byte, 1),
	}
	if !engine.AddBot(bot) {
		t.Fatal("bot admission failed")
	}
	wantErr := errors.New("temporary cosmetics read failure")
	current, err := refreshAdmittedBotCosmetics(context.Background(), engine, bot.BotID, func(context.Context, string) (map[string]string, error) {
		return nil, wantErr
	})
	if !current || !errors.Is(err, wantErr) {
		t.Fatalf("post-admission failure = current %t, err %v", current, err)
	}
	if len(engine.Bots[bot.BotID].Cosmetics) != 0 {
		t.Fatalf("read failure retained potentially revoked visuals: %+v", engine.Bots[bot.BotID].Cosmetics)
	}
}

type loadoutConfirmation struct {
	Type   string `json:"type"`
	Weapon string `json:"weapon"`
}

func runAdmissionConfirmationHarness(t *testing.T, engine *game.GameEngine, botID string) ([]loadoutConfirmation, int) {
	t.Helper()
	type harnessResult struct {
		err    error
		queued int
	}
	resultCh := make(chan harnessResult, 1)
	cfg := config.C
	cfg.WSMessageMaxBytes = 1024
	cfg.LoadoutTimeoutSecs = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			resultCh <- harnessResult{err: err}
			return
		}
		defer conn.Close()
		bot := &game.BotState{
			BotID:            botID,
			APIKeyID:         "key-1",
			Name:             "Single Confirmation",
			Weapon:           "sword",
			Stats:            map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			FallbackBehavior: "aggressive",
			SendChan:         make(chan []byte, 4),
		}
		if err := handleLoadoutPhase(conn, bot, &cfg); err != nil {
			resultCh <- harnessResult{err: err}
			return
		}
		if !engine.AddBot(bot) {
			resultCh <- harnessResult{err: errors.New("bot admission failed")}
			return
		}
		confirmation, current := engine.BuildLoadoutConfirmationForSession(bot.BotID, bot)
		if !current {
			resultCh <- harnessResult{err: errors.New("admitted session was replaced before confirmation")}
			return
		}
		if err := conn.WriteJSON(confirmation); err != nil {
			resultCh <- harnessResult{err: err, queued: len(bot.SendChan)}
			return
		}
		resultCh <- harnessResult{queued: len(bot.SendChan)}
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial admission harness: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set admission read deadline: %v", err)
	}
	if err := conn.WriteJSON(map[string]interface{}{
		"type":              "select_loadout",
		"weapon":            "staff",
		"stats":             map[string]int{"hp": 1, "speed": 1, "attack": 8, "defense": 10},
		"fallback_behavior": "aggressive",
	}); err != nil {
		t.Fatalf("write selected loadout: %v", err)
	}

	var confirmations []loadoutConfirmation
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) && closeErr.Code == websocket.CloseNormalClosure {
				break
			}
			t.Fatalf("read admission confirmation: %v", err)
		}
		var message loadoutConfirmation
		if err := json.Unmarshal(payload, &message); err != nil {
			t.Fatalf("decode admission message %q: %v", payload, err)
		}
		if message.Type == "loadout_confirmed" {
			confirmations = append(confirmations, message)
		}
	}
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("admission harness failed: %v", result.err)
	}
	return confirmations, result.queued
}

func requireWebSocketCloseCode(t *testing.T, conn *websocket.Conn, want int) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err == nil {
		t.Fatalf("read application payload %q, want close code %d", payload, want)
	}
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != want {
		t.Fatalf("read error = %v, want websocket close code %d", err, want)
	}
}
