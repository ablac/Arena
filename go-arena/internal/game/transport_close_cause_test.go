package game

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"arena-server/internal/config"

	"github.com/gorilla/websocket"
)

func testBotWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade bot transport harness: %v", err)
			return
		}
		serverConnCh <- conn
	}))
	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial bot transport harness: %v", err)
	}
	serverConn := <-serverConnCh
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		server.Close()
	})
	return serverConn, clientConn
}

func requireBotTransportCause(t *testing.T, signal <-chan BotTransportCloseCause, source string, code int, reason string) {
	t.Helper()
	select {
	case cause := <-signal:
		if cause.Source != source || cause.CloseCode != code || cause.CloseReason != reason {
			t.Fatalf("transport close cause = %+v, want source=%q code=%d reason=%q", cause, source, code, reason)
		}
	default:
		t.Fatalf("transport close cause %q was not published", source)
	}
}

func TestAddBotSignalsReplacedTransportAndPreservesNewSignal(t *testing.T) {
	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	oldSignal := make(chan BotTransportCloseCause, 1)
	newSignal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Old Session",
		TransportCloseCause: oldSignal,
	}
	replacement := &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "New Session",
		TransportCloseCause: newSignal,
	}

	if !engine.AddBot(replacement) {
		t.Fatal("replacement admission failed")
	}
	if replacement.TransportCloseCause != newSignal {
		t.Fatal("active reconnect copied the old session's close-cause signal")
	}
	select {
	case cause := <-oldSignal:
		if cause.Source != "session_replaced" {
			t.Fatalf("replacement source = %q, want session_replaced", cause.Source)
		}
		if cause.CloseCode != websocket.CloseNormalClosure {
			t.Fatalf("replacement close code = %d, want %d", cause.CloseCode, websocket.CloseNormalClosure)
		}
		if !strings.Contains(cause.CloseReason, "replaced") {
			t.Fatalf("replacement reason = %q, want replacement detail", cause.CloseReason)
		}
	default:
		t.Fatal("replaced session did not receive a close cause")
	}
}

func TestCheckAFKSignalsServerPolicyBeforeRemoval(t *testing.T) {
	previousTimeout := config.C.AFKTimeoutTicks
	config.C.AFKTimeoutTicks = 1
	t.Cleanup(func() { config.C.AFKTimeoutTicks = previousTimeout })

	engine := NewGameEngine()
	engine.Round.Phase = PhaseActive
	engine.Round.StartTick = 1
	engine.TickCount = 10
	signal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Silent Bot", IsAlive: true,
		TransportCloseCause: signal,
	}

	engine.checkAFK()

	select {
	case cause := <-signal:
		if cause.Source != "server_policy" {
			t.Fatalf("AFK source = %q, want server_policy", cause.Source)
		}
		if cause.CloseCode != websocket.ClosePolicyViolation {
			t.Fatalf("AFK close code = %d, want %d", cause.CloseCode, websocket.ClosePolicyViolation)
		}
		if cause.CloseReason != "AFK timeout" {
			t.Fatalf("AFK close reason = %q, want AFK timeout", cause.CloseReason)
		}
	default:
		t.Fatal("AFK removal did not publish a server-policy cause")
	}
}

func TestDisconnectBotSessionForKeySignalsServerPolicy(t *testing.T) {
	serverConn, _ := testBotWebSocketPair(t)
	engine := NewGameEngine()
	signal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Locked Bot", Conn: serverConn,
		TransportCloseCause: signal,
	}

	if !engine.DisconnectBotSessionForKey("bot-1", "key-1") {
		t.Fatal("protocol-lock disconnect did not find the session")
	}
	requireBotTransportCause(t, signal, "server_policy", websocket.ClosePolicyViolation, "temporary protocol lock")
}

func TestKickBotSignalsServerKick(t *testing.T) {
	serverConn, _ := testBotWebSocketPair(t)
	engine := NewGameEngine()
	signal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Kicked Bot", Conn: serverConn,
		TransportCloseCause: signal,
	}

	if !engine.KickBot("bot-1", "API key revoked") {
		t.Fatal("server kick did not find the session")
	}
	requireBotTransportCause(t, signal, "server_kick", websocket.ClosePolicyViolation, "API key revoked")
}

func TestKickBotBoundsLongUTF8CloseReason(t *testing.T) {
	serverConn, clientConn := testBotWebSocketPair(t)
	engine := NewGameEngine()
	signal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Kicked Bot", Conn: serverConn,
		TransportCloseCause: signal,
	}
	longReason := strings.Repeat("界", 100)

	if !engine.KickBot("bot-1", longReason) {
		t.Fatal("server kick did not find the session")
	}
	_, _, err := clientConn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("long-reason kick read error = %v, want WebSocket close error", err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("long-reason close code = %d, want %d", closeErr.Code, websocket.ClosePolicyViolation)
	}
	if len(closeErr.Text) > 123 || !utf8.ValidString(closeErr.Text) {
		t.Fatalf("long-reason close text is not bounded valid UTF-8: %q (%d bytes)", closeErr.Text, len(closeErr.Text))
	}
	if closeErr.Text == "" {
		t.Fatal("long-reason kick discarded the close reason")
	}
	requireBotTransportCause(t, signal, "server_kick", websocket.ClosePolicyViolation, closeErr.Text)
}

func TestCloseAllWebSocketsSignalsServiceRestart(t *testing.T) {
	serverConn, _ := testBotWebSocketPair(t)
	engine := NewGameEngine()
	signal := make(chan BotTransportCloseCause, 1)
	engine.Bots["bot-1"] = &BotState{
		BotID: "bot-1", APIKeyID: "key-1", Name: "Restarted Bot", Conn: serverConn,
		TransportCloseCause: signal,
	}

	engine.CloseAllWebSockets("service restart")

	requireBotTransportCause(t, signal, "service_restart", websocket.CloseServiceRestart, "service restart")
}
