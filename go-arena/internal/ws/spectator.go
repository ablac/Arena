package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/gorilla/websocket"
)

var spectatorWriteBufferPool sync.Pool

type spectatorRateLimitCheck func(context.Context, string, int, int) (bool, int, time.Time, error)

// spectatorRateLimitChecker is replaceable in focused pre-upgrade tests.
var spectatorRateLimitChecker spectatorRateLimitCheck = security.CheckRateLimit

const (
	// How often to send WebSocket ping frames to spectators.
	spectatorPingInterval = 30 * time.Second
	// How long to wait for a pong before considering the connection dead.
	spectatorPongTimeout = 60 * time.Second
	// Application-level heartbeats are visible to browser JavaScript, unlike
	// WebSocket ping frames, and keep its stale-stream timer healthy while the
	// game is paused and no arena states are being broadcast.
	spectatorHeartbeatInterval = 10 * time.Second
	spectatorWriteTimeout      = 10 * time.Second
	// Public spectators intentionally trail live gameplay so a competing bot
	// cannot use the full-state rendering feed as a real-time radar. Round-end
	// presentation stays in that ordered stream; service-status updates and
	// heartbeats remain immediate.
	spectatorStateDelay = 5 * time.Second
	// This covers the five-second delay window at the supported 60 Hz maximum,
	// plus scheduling headroom, while keeping a stalled connection's memory
	// bounded. One slot is reserved for the terminal round_end message.
	maxDelayedSpectatorMessages = 384
	// A full server asks browsers to retry shortly rather than paying for a
	// WebSocket upgrade known to have no available spectator slot.
	spectatorCapacityRetryAfterSeconds = 5
)

func spectatorUpgradeRateLimit(ip string) (string, int) {
	return "ws:spectator:connect:" + ip, config.C.WSSpectatorConnectRatePerMin
}

// spectatorUpgrader is the shared WebSocket upgrader for spectator connections.
var spectatorUpgrader = websocket.Upgrader{
	HandshakeTimeout:  5 * time.Second,
	ReadBufferSize:    1024,
	WriteBufferSize:   4096,
	WriteBufferPool:   &spectatorWriteBufferPool,
	EnableCompression: true,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

// SpectatorHandler returns an http.HandlerFunc that manages spectator
// WebSocket connections. Spectators receive broadcast game state from the
// engine but do not send meaningful messages.
func SpectatorHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admission := beginWebSocketAdmission(websocketEndpointSpectator)
		defer admission.finish()

		cfg := &config.C
		clientIP := security.ExtractClientIP(r)

		// IP bans are shared by bot and spectator admission. The admin panel's
		// spectator "Ban IP" action would otherwise disconnect only the current
		// socket while allowing the same client to reconnect immediately.
		if engine.IsIPBanned(clientIP) {
			admission.fail(websocketFailureAuth)
			http.Error(w, "IP banned", http.StatusForbidden)
			return
		}

		// This is an inexpensive best-effort guard for an already-full server.
		// TryAddSpectator remains authoritative after upgrade because capacity can
		// change between this snapshot and registration.
		if engine.SpectatorCount() >= cfg.MaxSpectators {
			admission.fail(websocketFailureCapacity)
			w.Header().Set("Retry-After", strconv.Itoa(spectatorCapacityRetryAfterSeconds))
			http.Error(w, "spectator limit reached", http.StatusServiceUnavailable)
			return
		}

		// Bound anonymous upgrade work before Gorilla allocates a connection.
		// Loopback remains exempt for local demos and health/smoke tooling.
		if cfg.WSSpectatorConnectRatePerMin > 0 && !isLoopbackIP(clientIP) {
			limitKey, limit := spectatorUpgradeRateLimit(clientIP)
			allowed, _, _, err := spectatorRateLimitChecker(r.Context(), limitKey, limit, 60)
			if err != nil {
				slog.Warn("spectator websocket rate limit check error, allowing", "error", err, "ip", clientIP)
			} else if !allowed {
				admission.fail(websocketFailureRateLimit)
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many spectator connections", http.StatusTooManyRequests)
				return
			}
		}

		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			admission.fail(websocketFailureUpgrade)
			slog.Error("spectator websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
			return
		}
		admission.upgraded()

		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic in spectator handler", "recover", p)
			}
			conn.Close()
		}()

		// Create spectator connection with buffered send channel.
		spec := &game.SpectatorConn{
			Conn:         conn,
			SendChan:     make(chan *game.SpectatorMessage, game.SpectatorSendBufferSize),
			StateChan:    make(chan *game.SpectatorMessage, 1),
			RoundEndChan: make(chan game.SpectatorRoundEndBatch, 1),
			Done:         make(chan struct{}),
			IP:           clientIP,
			ConnectedAt:  time.Now(),
		}

		// Admission and capacity checking must be one atomic engine operation;
		// otherwise simultaneous upgrades can all pass a separate count check.
		if !engine.TryAddSpectator(spec, cfg.MaxSpectators) {
			admission.fail(websocketFailureCapacity)
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "spectator limit reached"),
				time.Now().Add(spectatorWriteTimeout),
			)
			return
		}
		admission.admitted()
		slog.Info("spectator connected", "remote", r.RemoteAddr)
		engine.SendServiceStatusToSpectator(spec)

		// Start writer goroutine (includes periodic ping).
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go spectatorWriter(ctx, spec, engine.IsPaused)

		// Reader loop: read and discard messages to keep the connection alive
		// and detect disconnects.
		spectatorReader(conn)

		// Cleanup on disconnect.
		cancel()
		engine.RemoveSpectator(spec)
		spec.CloseDone()
		close(spec.SendChan)

		slog.Info("spectator disconnected", "remote", r.RemoteAddr)
	}
}

// spectatorReader detects when the spectator disconnects. The public stream
// is receive-only; application data from a spectator closes the connection
// instead of providing a second, undocumented command channel.
func spectatorReader(conn *websocket.Conn) {
	// Set a generous read limit -- spectators should not send large messages.
	conn.SetReadLimit(512)

	// Set initial read deadline; reset on each pong.
	conn.SetReadDeadline(time.Now().Add(spectatorPongTimeout))

	// Handle pong messages: reset the read deadline so the connection stays
	// alive as long as the client responds to our pings.
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(spectatorPongTimeout))
		return nil
	})

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("spectator read error", "error", err)
			}
			return
		}
		// Older browser bundles send this application heartbeat every 15
		// seconds. Keep it as an exact compatibility no-op so cached clients do
		// not reconnect forever; every other application message remains denied.
		if messageType == websocket.TextMessage && bytes.Equal(payload, []byte("ping")) {
			conn.SetReadDeadline(time.Now().Add(spectatorPongTimeout))
			continue
		}
		slog.Warn("spectator sent unexpected application data; closing receive-only stream")
		return
	}
}

// spectatorWriter drains the spectator's SendChan and writes each message to
// the WebSocket connection. It also sends periodic WebSocket ping frames to
// keep the connection alive through reverse proxies.
func spectatorWriter(ctx context.Context, spec *game.SpectatorConn, isPaused func() bool) {
	spectatorWriterWithIntervals(ctx, spec, isPaused, spectatorPingInterval, spectatorHeartbeatInterval)
}

func spectatorWriterWithIntervals(ctx context.Context, spec *game.SpectatorConn, isPaused func() bool, pingInterval, heartbeatInterval time.Duration) {
	spectatorWriterWithTimings(ctx, spec, isPaused, pingInterval, heartbeatInterval, spectatorStateDelay)
}

type delayedSpectatorMessage struct {
	message   *game.SpectatorMessage
	releaseAt time.Time
}

func spectatorWriterWithTimings(ctx context.Context, spec *game.SpectatorConn, isPaused func() bool, pingInterval, heartbeatInterval, stateDelay time.Duration) {
	pingTicker := time.NewTicker(pingInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	delayTimer := time.NewTimer(time.Hour)
	if !delayTimer.Stop() {
		<-delayTimer.C
	}
	var delayReady <-chan time.Time
	delayed := make([]delayedSpectatorMessage, 0, 64)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()
	defer delayTimer.Stop()
	defer spec.Conn.Close()
	handleMessage := func(msg *game.SpectatorMessage) bool {
		if msg == nil {
			return true
		}
		if stateDelay > 0 && isDelayedSpectatorState(msg.Payload) {
			roundEnd := isRoundEndSpectatorState(msg.Payload)
			if !roundEnd && len(delayed) >= maxDelayedSpectatorMessages-1 {
				// Coalesce the oldest replaceable snapshot so the newest/final
				// arena state remains immediately before round_end. Appending
				// with a fresh release time preserves the anti-radar delay.
				var evicted bool
				delayed, evicted = evictOldestDelayedArenaState(delayed)
				if !evicted {
					return true
				}
			}
			if roundEnd && len(delayed) >= maxDelayedSpectatorMessages {
				// Keep the newest lifecycle boundary without allowing the
				// queue to grow. A normal stream always has a replaceable arena
				// state here; the fallback handles malformed terminal floods.
				var evicted bool
				delayed, evicted = evictOldestDelayedArenaState(delayed)
				if !evicted {
					delayed[0].message = nil
					copy(delayed, delayed[1:])
					delayed[len(delayed)-1] = delayedSpectatorMessage{}
					delayed = delayed[:len(delayed)-1]
				}
			}
			delayed = append(delayed, delayedSpectatorMessage{
				message:   msg,
				releaseAt: time.Now().Add(stateDelay),
			})
			if len(delayed) == 1 {
				delayTimer.Reset(stateDelay)
				delayReady = delayTimer.C
			}
			return true
		}
		if err := writePreparedSpectatorMessage(spec.Conn, msg); err != nil {
			slog.Warn("spectator write error", "error", err)
			return false
		}
		return true
	}
	handleRoundEnd := func(batch game.SpectatorRoundEndBatch) bool {
		return handleMessage(batch.FinalState) && handleMessage(batch.RoundEnd)
	}

	for {
		// Lifecycle batches outrank later lobby/control backlog. The batch itself
		// always applies its captured final state before round_end.
		select {
		case batch := <-spec.RoundEndChan:
			if !handleRoundEnd(batch) {
				return
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			_ = writeSpectatorMessage(spec.Conn,
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return

		case <-pingTicker.C:
			// Send a WebSocket ping frame to keep the connection alive.
			if err := writeSpectatorMessage(spec.Conn, websocket.PingMessage, nil); err != nil {
				slog.Warn("spectator ping error", "error", err)
				return
			}

		case now := <-heartbeatTicker.C:
			paused := false
			if isPaused != nil {
				paused = isPaused()
			}
			if err := writeSpectatorMessage(spec.Conn, websocket.TextMessage, spectatorHeartbeatMessage(paused, now)); err != nil {
				slog.Warn("spectator heartbeat error", "error", err)
				return
			}

		case now := <-delayReady:
			for len(delayed) > 0 && !delayed[0].releaseAt.After(now) {
				if err := writePreparedSpectatorMessage(spec.Conn, delayed[0].message); err != nil {
					slog.Warn("spectator delayed-state write error", "error", err)
					return
				}
				delayed[0].message = nil
				delayed = delayed[1:]
			}
			if len(delayed) == 0 {
				delayed = delayed[:0]
				delayReady = nil
			} else {
				delayTimer.Reset(time.Until(delayed[0].releaseAt))
				delayReady = delayTimer.C
			}

		case batch := <-spec.RoundEndChan:
			if !handleRoundEnd(batch) {
				return
			}

		case msg := <-spec.StateChan:
			if !handleMessage(msg) {
				return
			}

		case msg, ok := <-spec.SendChan:
			if !ok {
				// Channel closed.
				_ = writeSpectatorMessage(spec.Conn,
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}
			// If a lifecycle batch arrived concurrently with this normal frame,
			// queue it first so later lobby traffic cannot cross the boundary.
			select {
			case batch := <-spec.RoundEndChan:
				if !handleRoundEnd(batch) {
					return
				}
			default:
			}
			if !handleMessage(msg) {
				return
			}
		}
	}
}

func evictOldestDelayedArenaState(delayed []delayedSpectatorMessage) ([]delayedSpectatorMessage, bool) {
	for i := range delayed {
		if delayed[i].message == nil || !isArenaSpectatorState(delayed[i].message.Payload) {
			continue
		}
		delayed[i].message = nil
		copy(delayed[i:], delayed[i+1:])
		delayed[len(delayed)-1] = delayedSpectatorMessage{}
		return delayed[:len(delayed)-1], true
	}
	return delayed, false
}

func isDelayedSpectatorState(payload []byte) bool {
	// SpectatorState and RoundEndSpectatorMessage are marshaled from structs
	// whose Type field is first, so keep their hot-path checks at the prefix.
	// Lobby state is marshaled from a map, where encoding/json sorts keys and
	// may place type later in the payload.
	return isArenaSpectatorState(payload) ||
		isRoundEndSpectatorState(payload) ||
		bytes.Contains(payload, []byte(`"type":"lobby_state"`))
}

func isArenaSpectatorState(payload []byte) bool {
	return bytes.HasPrefix(payload, []byte(`{"type":"arena_state"`))
}

func isRoundEndSpectatorState(payload []byte) bool {
	return bytes.HasPrefix(payload, []byte(`{"type":"round_end"`))
}

type spectatorHeartbeat struct {
	Type       string `json:"type"`
	Paused     bool   `json:"paused"`
	ServerTime int64  `json:"server_time"`
}

func spectatorHeartbeatMessage(paused bool, now time.Time) []byte {
	payload, _ := json.Marshal(spectatorHeartbeat{
		Type:       "heartbeat",
		Paused:     paused,
		ServerTime: now.UnixMilli(),
	})
	return payload
}

func writeSpectatorMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(spectatorWriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(messageType, payload)
}

func writePreparedSpectatorMessage(conn *websocket.Conn, message *game.SpectatorMessage) error {
	if message == nil || message.Prepared == nil {
		return errors.New("spectator message is not prepared")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(spectatorWriteTimeout)); err != nil {
		return err
	}
	return conn.WritePreparedMessage(message.Prepared)
}
