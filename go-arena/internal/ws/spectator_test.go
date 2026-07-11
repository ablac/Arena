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

func TestSpectatorWriterDelaysArenaStateButNotControlMessages(t *testing.T) {
	const stateDelay = 80 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan []byte, 2), Done: make(chan struct{}),
		}
		spec.SendChan <- []byte(`{"type":"service_status","paused":false}`)
		spec.SendChan <- []byte(`{"type":"arena_state","tick":42}`)
		spectatorWriterWithTimings(
			context.Background(), spec, func() bool { return false },
			time.Hour, time.Hour, stateDelay,
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

	started := time.Now()
	_, immediate, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read immediate spectator message: %v", err)
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(immediate, &envelope); err != nil {
		t.Fatalf("decode spectator message %q: %v", immediate, err)
	}
	if envelope.Type != "service_status" {
		t.Fatalf("first message type = %q, want immediate service_status", envelope.Type)
	}
	if elapsed := time.Since(started); elapsed >= stateDelay {
		t.Fatalf("service status was delayed for %v", elapsed)
	}

	_, delayed, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read delayed spectator message: %v", err)
	}
	if err := json.Unmarshal(delayed, &envelope); err != nil {
		t.Fatalf("decode spectator message %q: %v", delayed, err)
	}
	if envelope.Type != "arena_state" {
		t.Fatalf("second message type = %q, want delayed arena_state", envelope.Type)
	}
	if elapsed := time.Since(started); elapsed < stateDelay-10*time.Millisecond {
		t.Fatalf("arena state released after %v, want at least %v", elapsed, stateDelay)
	}
}

func TestSpectatorWriterPreservesGameplayStateOrderAcrossRoundEnd(t *testing.T) {
	const stateDelay = 80 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan []byte, 2), Done: make(chan struct{}),
		}
		spec.SendChan <- []byte(`{"type":"arena_state","tick":99}`)
		// Lobby state is marshaled from a map, so type is not guaranteed to be
		// the first JSON field. The classifier must still treat it as gameplay.
		spec.SendChan <- []byte(`{"bots_connected":3,"type":"lobby_state"}`)
		spectatorWriterWithTimings(
			ctx, spec, func() bool { return false },
			time.Hour, time.Hour, stateDelay,
		)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial spectator writer: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	started := time.Now()
	var got []string
	for range 2 {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read delayed gameplay state: %v", err)
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode spectator message %q: %v", payload, err)
		}
		got = append(got, envelope.Type)
	}
	if got[0] != "arena_state" || got[1] != "lobby_state" {
		t.Fatalf("gameplay state order = %v, want [arena_state lobby_state]", got)
	}
	if elapsed := time.Since(started); elapsed < stateDelay-15*time.Millisecond {
		t.Fatalf("lobby state bypassed presentation delay after %v", elapsed)
	}
}

func TestSpectatorReaderRejectsApplicationMessages(t *testing.T) {
	readerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		spectatorReader(conn)
		close(readerDone)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial spectator reader: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"action"}`)); err != nil {
		t.Fatalf("write unexpected spectator message: %v", err)
	}

	select {
	case <-readerDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("spectator reader accepted an application message")
	}
}

func TestSpectatorReaderAllowsLegacyBrowserPing(t *testing.T) {
	readerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		spectatorReader(conn)
		close(readerDone)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial spectator reader: %v", err)
	}
	defer conn.Close()

	// Deployed browser bundles send this legacy application heartbeat every
	// 15 seconds. It must remain a no-op while all command-shaped spectator
	// messages continue to close the receive-only stream.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write legacy browser ping: %v", err)
	}
	select {
	case <-readerDone:
		t.Fatal("legacy browser ping closed the spectator stream")
	case <-time.After(75 * time.Millisecond):
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"action"}`)); err != nil {
		t.Fatalf("write unexpected spectator message: %v", err)
	}
	select {
	case <-readerDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("spectator reader accepted a command-shaped application message")
	}
}
