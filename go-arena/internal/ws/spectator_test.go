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

func TestSpectatorHandler_RejectsAdminBannedIPBeforeUpgrade(t *testing.T) {
	engine := game.NewGameEngine()
	engine.BanIP("203.0.113.25")

	req := httptest.NewRequest(http.MethodGet, "/ws/spectator", nil)
	req.Header.Set("CF-Connecting-IP", "203.0.113.25")
	rec := httptest.NewRecorder()

	SpectatorHandler(engine).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "IP banned") {
		t.Errorf("body = %q, want IP-ban explanation", rec.Body.String())
	}
	if got := engine.SpectatorCount(); got != 0 {
		t.Errorf("spectator count = %d, want 0", got)
	}
}

func TestSpectatorHandlerRejectsConnectionBeyondAtomicCap(t *testing.T) {
	previousMax := config.C.MaxSpectators
	config.C.MaxSpectators = 1
	t.Cleanup(func() { config.C.MaxSpectators = previousMax })

	engine := game.NewGameEngine()
	server := httptest.NewServer(SpectatorHandler(engine))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial first spectator: %v", err)
	}
	defer first.Close()
	deadline := time.Now().Add(time.Second)
	for engine.SpectatorCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := engine.SpectatorCount(); got != 1 {
		t.Fatalf("spectator count after first dial = %d, want 1", got)
	}

	second, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("second connection should upgrade before capacity close: %v", err)
	}
	defer second.Close()
	if err := second.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set second read deadline: %v", err)
	}
	_, _, err = second.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("second connection error = %v, want close code %d", err, websocket.CloseTryAgainLater)
	}
	if got := engine.SpectatorCount(); got != 1 {
		t.Fatalf("rejected connection changed count to %d", got)
	}
}

func TestSpectatorWriterSendsPausedHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan []byte, 1), Done: make(chan struct{}),
		}
		spectatorWriterWithIntervals(
			context.Background(), spec, func() bool { return true },
			time.Hour, 5*time.Millisecond,
		)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial spectator writer: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if messageType != websocket.TextMessage {
		t.Fatalf("message type = %d, want text", messageType)
	}
	var heartbeat spectatorHeartbeat
	if err := json.Unmarshal(payload, &heartbeat); err != nil {
		t.Fatalf("decode heartbeat %q: %v", payload, err)
	}
	if heartbeat.Type != "heartbeat" || !heartbeat.Paused || heartbeat.ServerTime <= 0 {
		t.Fatalf("unexpected heartbeat: %+v", heartbeat)
	}
}
