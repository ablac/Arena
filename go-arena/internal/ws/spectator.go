package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/gorilla/websocket"
)

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
	// cannot use the full-state rendering feed as a real-time radar. Control
	// messages and service-status updates remain immediate.
	spectatorStateDelay = 5 * time.Second
	// At the normal 10 Hz broadcast rate this holds more than twice the
	// delayed window while keeping a stalled connection's memory bounded.
	maxDelayedSpectatorMessages = 128
)

// spectatorUpgrader is the shared WebSocket upgrader for spectator connections.
var spectatorUpgrader = websocket.Upgrader{
	ReadBufferSize:    1024,
	WriteBufferSize:   4096,
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
		cfg := &config.C
		clientIP := security.ExtractClientIP(r)

		// IP bans are shared by bot and spectator admission. The admin panel's
		// spectator "Ban IP" action would otherwise disconnect only the current
		// socket while allowing the same client to reconnect immediately.
		if engine.IsIPBanned(clientIP) {
			http.Error(w, "IP banned", http.StatusForbidden)
			return
		}

		conn, err := spectatorUpgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("spectator websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
			return
		}

		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic in spectator handler", "recover", p)
			}
			conn.Close()
		}()

		// Create spectator connection with buffered send channel.
		spec := &game.SpectatorConn{
			Conn:        conn,
			SendChan:    make(chan []byte, 32),
			Done:        make(chan struct{}),
			IP:          clientIP,
			ConnectedAt: time.Now(),
		}

		// Admission and capacity checking must be one atomic engine operation;
		// otherwise simultaneous upgrades can all pass a separate count check.
		if !engine.TryAddSpectator(spec, cfg.MaxSpectators) {
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "spectator limit reached"),
				time.Now().Add(spectatorWriteTimeout),
			)
			return
		}
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
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("spectator read error", "error", err)
			}
			return
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
	payload   []byte
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

	for {
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
				if err := writeSpectatorMessage(spec.Conn, websocket.TextMessage, delayed[0].payload); err != nil {
					slog.Warn("spectator delayed-state write error", "error", err)
					return
				}
				delayed[0].payload = nil
				delayed = delayed[1:]
			}
			if len(delayed) == 0 {
				delayed = delayed[:0]
				delayReady = nil
			} else {
				delayTimer.Reset(time.Until(delayed[0].releaseAt))
				delayReady = delayTimer.C
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

			if stateDelay > 0 && isDelayedSpectatorState(msg) {
				if len(delayed) >= maxDelayedSpectatorMessages {
					// Preserve the oldest/keyframe messages and shed only excess
					// newest frames until the writer catches up.
					continue
				}
				delayed = append(delayed, delayedSpectatorMessage{
					payload:   msg,
					releaseAt: time.Now().Add(stateDelay),
				})
				if len(delayed) == 1 {
					delayTimer.Reset(stateDelay)
					delayReady = delayTimer.C
				}
				continue
			}

			if err := writeSpectatorMessage(spec.Conn, websocket.TextMessage, msg); err != nil {
				slog.Warn("spectator write error", "error", err)
				return
			}
		}
	}
}

func isDelayedSpectatorState(payload []byte) bool {
	// SpectatorState is marshaled from a struct whose Type field is first, so
	// keep its hot-path check at the prefix. Lobby state is marshaled from a map,
	// where encoding/json sorts keys and may place type later in the payload.
	return bytes.HasPrefix(payload, []byte(`{"type":"arena_state"`)) ||
		bytes.Contains(payload, []byte(`"type":"lobby_state"`))
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
