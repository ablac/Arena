package ws

import (
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

// spectatorReader reads and discards messages from the spectator WebSocket.
// Its only purpose is to keep the connection alive and detect when the
// spectator disconnects.
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
		// Any message from the client also resets the read deadline.
		conn.SetReadDeadline(time.Now().Add(spectatorPongTimeout))
	}
}

// spectatorWriter drains the spectator's SendChan and writes each message to
// the WebSocket connection. It also sends periodic WebSocket ping frames to
// keep the connection alive through reverse proxies.
func spectatorWriter(ctx context.Context, spec *game.SpectatorConn, isPaused func() bool) {
	spectatorWriterWithIntervals(ctx, spec, isPaused, spectatorPingInterval, spectatorHeartbeatInterval)
}

func spectatorWriterWithIntervals(ctx context.Context, spec *game.SpectatorConn, isPaused func() bool, pingInterval, heartbeatInterval time.Duration) {
	pingTicker := time.NewTicker(pingInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()
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

		case msg, ok := <-spec.SendChan:
			if !ok {
				// Channel closed.
				_ = writeSpectatorMessage(spec.Conn,
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}

			if err := writeSpectatorMessage(spec.Conn, websocket.TextMessage, msg); err != nil {
				slog.Warn("spectator write error", "error", err)
				return
			}
		}
	}
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
