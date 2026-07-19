package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"

	"github.com/gorilla/websocket"
)

func testSpectatorMessage(t *testing.T, payload string) *game.SpectatorMessage {
	t.Helper()
	prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		t.Fatalf("prepare spectator test message: %v", err)
	}
	return &game.SpectatorMessage{Payload: []byte(payload), Prepared: prepared}
}

func TestSpectatorHandler_RejectsAdminBannedIPBeforeUpgrade(t *testing.T) {
	previousTrustedProxies := config.C.TrustedProxyCIDRs
	config.C.TrustedProxyCIDRs = "192.0.2.1/32"
	t.Cleanup(func() { config.C.TrustedProxyCIDRs = previousTrustedProxies })

	engine := game.NewGameEngine()
	engine.BanIP("203.0.113.25")

	req := httptest.NewRequest(http.MethodGet, "/ws/spectator", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.25")
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

func TestSpectatorHandlerRateLimitsByIPBeforeUpgrade(t *testing.T) {
	originalChecker := spectatorRateLimitChecker
	originalMetrics := defaultWebSocketAdmissionMetrics
	originalConnectLimit := config.C.WSSpectatorConnectRatePerMin
	originalMaxSpectators := config.C.MaxSpectators
	defaultWebSocketAdmissionMetrics = newWebSocketAdmissionMetrics(time.Now)
	config.C.WSSpectatorConnectRatePerMin = 60
	config.C.MaxSpectators = 500
	var called atomic.Int32
	spectatorRateLimitChecker = func(_ context.Context, key string, limit, window int) (bool, int, time.Time, error) {
		called.Add(1)
		if key != "ws:spectator:connect:203.0.113.44" {
			t.Fatalf("rate-limit key = %q", key)
		}
		if limit != 60 || window != 60 {
			t.Fatalf("rate limit = %d/%ds, want NAT-safe 60/60s", limit, window)
		}
		return false, 0, time.Now().Add(time.Minute), nil
	}
	t.Cleanup(func() {
		spectatorRateLimitChecker = originalChecker
		defaultWebSocketAdmissionMetrics = originalMetrics
		config.C.WSSpectatorConnectRatePerMin = originalConnectLimit
		config.C.MaxSpectators = originalMaxSpectators
	})

	engine := game.NewGameEngine()
	req := httptest.NewRequest(http.MethodGet, "/ws/spectator", nil)
	req.RemoteAddr = "203.0.113.44:4321"
	rec := httptest.NewRecorder()
	SpectatorHandler(engine).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	if called.Load() != 1 {
		t.Fatalf("rate-limit checks = %d, want 1", called.Load())
	}
	if engine.SpectatorCount() != 0 {
		t.Fatal("rate-limited spectator reached engine admission")
	}
	metrics := defaultWebSocketAdmissionMetrics.snapshot().Spectator.Totals
	if metrics.Attempts != 1 || metrics.Upgrades != 0 || metrics.Failures.RateLimit != 1 {
		t.Fatalf("spectator admission metrics = %#v, want pre-upgrade rate-limit failure", metrics)
	}
}

func TestSpectatorHandlerExemptsLoopbackFromConnectionLimit(t *testing.T) {
	originalChecker := spectatorRateLimitChecker
	originalConnectLimit := config.C.WSSpectatorConnectRatePerMin
	originalMaxSpectators := config.C.MaxSpectators
	config.C.WSSpectatorConnectRatePerMin = 60
	config.C.MaxSpectators = 500
	var called atomic.Int32
	spectatorRateLimitChecker = func(context.Context, string, int, int) (bool, int, time.Time, error) {
		called.Add(1)
		return false, 0, time.Now().Add(time.Minute), nil
	}
	t.Cleanup(func() {
		spectatorRateLimitChecker = originalChecker
		config.C.WSSpectatorConnectRatePerMin = originalConnectLimit
		config.C.MaxSpectators = originalMaxSpectators
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/spectator", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	SpectatorHandler(game.NewGameEngine()).ServeHTTP(rec, req)

	if called.Load() != 0 {
		t.Fatalf("loopback rate-limit checks = %d, want 0", called.Load())
	}
}

func TestSpectatorHandlerZeroDisablesConnectionLimit(t *testing.T) {
	originalChecker := spectatorRateLimitChecker
	originalConnectLimit := config.C.WSSpectatorConnectRatePerMin
	originalMaxSpectators := config.C.MaxSpectators
	config.C.WSSpectatorConnectRatePerMin = 0
	config.C.MaxSpectators = 500
	var called atomic.Int32
	spectatorRateLimitChecker = func(context.Context, string, int, int) (bool, int, time.Time, error) {
		called.Add(1)
		return false, 0, time.Now().Add(time.Minute), nil
	}
	t.Cleanup(func() {
		spectatorRateLimitChecker = originalChecker
		config.C.WSSpectatorConnectRatePerMin = originalConnectLimit
		config.C.MaxSpectators = originalMaxSpectators
	})

	req := httptest.NewRequest(http.MethodGet, "/ws/spectator", nil)
	req.RemoteAddr = "203.0.113.45:4321"
	rec := httptest.NewRecorder()
	SpectatorHandler(game.NewGameEngine()).ServeHTTP(rec, req)

	if called.Load() != 0 {
		t.Fatalf("disabled rate-limit checks = %d, want 0", called.Load())
	}
}

func TestSpectatorUpgradeRateLimitUsesNATSafeDefaultAndAllowsExplicitConfig(t *testing.T) {
	originalConnectLimit := config.C.WSSpectatorConnectRatePerMin
	t.Cleanup(func() { config.C.WSSpectatorConnectRatePerMin = originalConnectLimit })

	config.C.WSSpectatorConnectRatePerMin = 60
	key, limit := spectatorUpgradeRateLimit("203.0.113.7")
	if key != "ws:spectator:connect:203.0.113.7" || limit != 60 {
		t.Fatalf("spectator bucket = %q %d, want dedicated bucket with NAT-safe default", key, limit)
	}
	config.C.WSSpectatorConnectRatePerMin = 240
	_, limit = spectatorUpgradeRateLimit("203.0.113.7")
	if limit != 240 {
		t.Fatalf("configured spectator limit = %d, want 240", limit)
	}
}

func TestSpectatorUpgraderUsesSharedWriteBufferPool(t *testing.T) {
	if spectatorUpgrader.WriteBufferPool == nil {
		t.Fatal("spectator upgrader retains one write buffer per connection")
	}
}

func TestSpectatorHandlerRejectsKnownFullCapacityBeforeUpgrade(t *testing.T) {
	previousMax := config.C.MaxSpectators
	originalMetrics := defaultWebSocketAdmissionMetrics
	config.C.MaxSpectators = 1
	defaultWebSocketAdmissionMetrics = newWebSocketAdmissionMetrics(time.Now)
	t.Cleanup(func() {
		config.C.MaxSpectators = previousMax
		defaultWebSocketAdmissionMetrics = originalMetrics
	})

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

	second, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if second != nil {
		second.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second connection = (%v, %+v), want pre-upgrade HTTP 503", err, response)
	}
	if got := response.Header.Get("Retry-After"); got != "5" {
		t.Fatalf("capacity Retry-After = %q, want 5", got)
	}
	if got := engine.SpectatorCount(); got != 1 {
		t.Fatalf("rejected connection changed count to %d", got)
	}
	metrics := defaultWebSocketAdmissionMetrics.snapshot().Spectator.Totals
	if metrics.Attempts != 2 || metrics.Upgrades != 1 || metrics.Admissions != 1 || metrics.Failures.Capacity != 1 {
		t.Fatalf("spectator admission metrics = %#v, want one admission and one pre-upgrade capacity failure", metrics)
	}
}

func TestSpectatorWriterSendsPausedHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan *game.SpectatorMessage, 1), Done: make(chan struct{}),
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

func TestSpectatorWriterUsesPreparedMessageForQueuedFrames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		preparedPayload := []byte(`{"type":"service_status","source":"prepared"}`)
		prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, preparedPayload)
		if err != nil {
			t.Errorf("prepare spectator message: %v", err)
			return
		}
		spec := &game.SpectatorConn{
			Conn:     conn,
			SendChan: make(chan *game.SpectatorMessage, 1),
			Done:     make(chan struct{}),
		}
		spec.SendChan <- &game.SpectatorMessage{
			Payload:  []byte(`{"type":"service_status","source":"raw"}`),
			Prepared: prepared,
		}
		spectatorWriterWithTimings(
			ctx, spec, func() bool { return false },
			time.Hour, time.Hour, 0,
		)
	}))
	defer server.Close()

	dialer := websocket.Dialer{EnableCompression: true}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial spectator writer: %v", err)
	}
	defer func() {
		cancel()
		_ = conn.Close()
	}()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read queued spectator frame: %v", err)
	}
	var envelope struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode queued spectator frame %q: %v", payload, err)
	}
	if envelope.Source != "prepared" {
		t.Fatalf("spectator writer sent %q payload, want prepared frame", envelope.Source)
	}
}

func TestSpectatorWriterDelaysArenaStateButNotControlMessages(t *testing.T) {
	const stateDelay = 80 * time.Millisecond
	serviceMessage := testSpectatorMessage(t, `{"type":"service_status","paused":false}`)
	arenaMessage := testSpectatorMessage(t, `{"type":"arena_state","tick":42}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan *game.SpectatorMessage, 2), Done: make(chan struct{}),
		}
		spec.SendChan <- serviceMessage
		spec.SendChan <- arenaMessage
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
			Conn:         conn,
			SendChan:     make(chan *game.SpectatorMessage, game.SpectatorSendBufferSize),
			StateChan:    make(chan *game.SpectatorMessage, 1),
			RoundEndChan: make(chan game.SpectatorRoundEndBatch, 1),
			Done:         make(chan struct{}),
		}
		game.BroadcastToSpectators([]*game.SpectatorConn{spec}, []byte(`{"type":"arena_state","tick":99}`))
		game.BroadcastToSpectators([]*game.SpectatorConn{spec}, []byte(`{"type":"round_end","round_number":7}`))
		// Lobby state is marshaled from a map, so type is not guaranteed to be
		// the first JSON field. The classifier must still treat it as gameplay.
		game.BroadcastToSpectators([]*game.SpectatorConn{spec}, []byte(`{"bots_connected":3,"type":"lobby_state"}`))
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
	for range 3 {
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
	if got[0] != "arena_state" || got[1] != "round_end" || got[2] != "lobby_state" {
		t.Fatalf("gameplay state order = %v, want [arena_state round_end lobby_state]", got)
	}
	if elapsed := time.Since(started); elapsed < stateDelay-15*time.Millisecond {
		t.Fatalf("lobby state bypassed presentation delay after %v", elapsed)
	}
}

func TestSpectatorWriterRetainsRoundEndWhenDelayedQueueIsSaturated(t *testing.T) {
	const stateDelay = 150 * time.Millisecond
	const arenaFrames = maxDelayedSpectatorMessages + 17
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		spec := &game.SpectatorConn{
			Conn: conn, SendChan: make(chan *game.SpectatorMessage, arenaFrames+2), Done: make(chan struct{}),
		}
		for tick := range arenaFrames {
			spec.SendChan <- testSpectatorMessage(t, fmt.Sprintf(`{"type":"arena_state","tick":%d}`, tick))
		}
		spec.SendChan <- testSpectatorMessage(t, `{"type":"round_end","round_number":7}`)
		spec.SendChan <- testSpectatorMessage(t, `{"type":"service_status","paused":false}`)
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
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	started := time.Now()
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read immediate service status: %v", err)
	}
	var immediate struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &immediate); err != nil {
		t.Fatalf("decode immediate spectator message %q: %v", payload, err)
	}
	if immediate.Type != "service_status" || time.Since(started) >= stateDelay {
		t.Fatalf("saturated queue delayed service status: type=%q elapsed=%v", immediate.Type, time.Since(started))
	}

	roundEnds := 0
	lastType := ""
	lastArenaTick := -1
	arenaTickBeforeRoundEnd := -1
	roundEndElapsed := time.Duration(0)
	for range maxDelayedSpectatorMessages {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read saturated delayed queue: %v", err)
		}
		var envelope struct {
			Type string `json:"type"`
			Tick int    `json:"tick"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode spectator message %q: %v", payload, err)
		}
		if envelope.Type == "round_end" {
			roundEnds++
			arenaTickBeforeRoundEnd = lastArenaTick
			roundEndElapsed = time.Since(started)
		} else if envelope.Type == "arena_state" {
			lastArenaTick = envelope.Tick
		}
		lastType = envelope.Type
	}
	if roundEnds != 1 || lastType != "round_end" || arenaTickBeforeRoundEnd != arenaFrames-1 {
		t.Fatalf(
			"saturated queue delivered round_end count=%d last=%q after arena tick=%d, want one terminal message after newest tick %d",
			roundEnds, lastType, arenaTickBeforeRoundEnd, arenaFrames-1,
		)
	}
	if roundEndElapsed < stateDelay-15*time.Millisecond {
		t.Fatalf("saturated round_end bypassed presentation delay after %v", roundEndElapsed)
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
