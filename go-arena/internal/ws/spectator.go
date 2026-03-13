package ws

import (
	"context"
	"log/slog"
	"net/http"

	"arena-server/internal/config"
	"arena-server/internal/game"

	"github.com/gorilla/websocket"
)

// spectatorUpgrader is the shared WebSocket upgrader for spectator connections.
var spectatorUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
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

		// Check spectator limit before upgrading.
		if engine.SpectatorCount() >= cfg.MaxSpectators {
			http.Error(w, "spectator limit reached", http.StatusServiceUnavailable)
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
			Conn:     conn,
			SendChan: make(chan []byte, 32),
			Done:     make(chan struct{}),
		}

		// Register with the engine.
		engine.AddSpectator(spec)
		slog.Info("spectator connected", "remote", r.RemoteAddr)

		// Start writer goroutine.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go spectatorWriter(ctx, spec)

		// Reader loop: read and discard messages to keep the connection alive
		// and detect disconnects.
		spectatorReader(conn)

		// Cleanup on disconnect.
		cancel()
		engine.RemoveSpectator(spec)
		close(spec.Done)
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

	// Handle pong messages to keep the connection alive.
	conn.SetPongHandler(func(string) error {
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
		// Discard all received messages.
	}
}

// spectatorWriter drains the spectator's SendChan and writes each message to
// the WebSocket connection. It returns when the context is cancelled or the
// send channel is closed.
func spectatorWriter(ctx context.Context, spec *game.SpectatorConn) {
	for {
		select {
		case <-ctx.Done():
			spec.Conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return

		case msg, ok := <-spec.SendChan:
			if !ok {
				// Channel closed.
				spec.Conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}

			if err := spec.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("spectator write error", "error", err)
				return
			}
		}
	}
}
