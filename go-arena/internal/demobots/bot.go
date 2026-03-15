package demobots

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// demoBot represents a single demo bot client that connects to the arena
// server via REST + WebSocket, exactly like a real SDK bot.
type demoBot struct {
	config    BotConfig
	serverURL string // e.g. "http://localhost:8000"
	apiKey    string
	logger    *slog.Logger
}

// newDemoBot creates a demoBot from a config and server URL.
func newDemoBot(cfg BotConfig, serverURL string) *demoBot {
	return &demoBot{
		config:    cfg,
		serverURL: serverURL,
		logger:    slog.With("demo_bot", cfg.Name),
	}
}

// register calls POST /api/v1/keys/generate to obtain an API key, then
// configures the bot name and avatar via PUT /api/v1/bot/config.
func (b *demoBot) register(ctx context.Context) error {
	// Generate a key.
	url := b.serverURL + "/api/v1/keys/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create register request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		APIKey string `json:"api_key"`
		BotID  string `json:"bot_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}
	b.apiKey = result.APIKey

	if len(b.apiKey) > 12 {
		b.logger.Info("registered demo bot", "key_prefix", b.apiKey[:12]+"...")
	} else {
		b.logger.Info("registered demo bot")
	}

	// Configure the bot name and avatar.
	if err := b.configure(ctx); err != nil {
		b.logger.Warn("failed to configure bot, continuing with defaults", "error", err)
	}

	return nil
}

// configure sends PUT /api/v1/bot/config to set the bot name and avatar color.
func (b *demoBot) configure(ctx context.Context) error {
	cfg := map[string]interface{}{
		"name":         b.config.Name,
		"avatar_color": b.config.Color,
	}

	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	url := b.serverURL + "/api/v1/bot/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Arena-Key", b.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("config failed: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	b.logger.Info("configured bot", "name", b.config.Name, "color", b.config.Color)
	return nil
}

// run is the main loop. It connects via WebSocket, handles the protocol, and
// reconnects with exponential backoff on disconnection. It respects the
// context for graceful shutdown.
func (b *demoBot) run(ctx context.Context) {
	backoff := 1.0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := b.session(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled; stop reconnecting.
				return
			}
			b.logger.Warn("session ended", "error", err, "reconnect_in", fmt.Sprintf("%.0fs", backoff))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(backoff * float64(time.Second))):
		}
		backoff = min(backoff*2, 30.0)
	}
}

// session runs a single WebSocket session: connect, loadout, tick loop.
func (b *demoBot) session(ctx context.Context) error {
	// Build the WebSocket URL.
	wsURL := httpToWS(b.serverURL) + "/ws/bot?key=" + b.apiKey

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// 1. Read "connected" message.
	msg, err := readJSON(conn)
	if err != nil {
		return fmt.Errorf("read connected: %w", err)
	}
	if msgType, _ := msg["type"].(string); msgType != "connected" {
		return fmt.Errorf("expected 'connected', got %q", msgType)
	}

	// 2. Send "select_loadout".
	fallback := b.config.Strategy
	switch fallback {
	case "aggressive", "defensive", "territorial", "opportunistic", "hunter":
		// valid
	default:
		fallback = "aggressive"
	}

	loadout := map[string]interface{}{
		"type":              "select_loadout",
		"weapon":            b.config.Weapon,
		"stats":             b.config.Stats,
		"fallback_behavior": fallback,
	}
	if err := conn.WriteJSON(loadout); err != nil {
		return fmt.Errorf("send loadout: %w", err)
	}

	// 3. Read "loadout_confirmed".
	msg, err = readJSON(conn)
	if err != nil {
		return fmt.Errorf("read loadout_confirmed: %w", err)
	}
	if msgType, _ := msg["type"].(string); msgType != "loadout_confirmed" {
		b.logger.Warn("expected 'loadout_confirmed'", "got", msgType)
	}

	b.logger.Info("entered arena", "weapon", b.config.Weapon, "strategy", b.config.Strategy)

	// 4. Main message loop.
	for {
		select {
		case <-ctx.Done():
			conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutting down"),
			)
			return ctx.Err()
		default:
		}

		msg, err := readJSON(conn)
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "tick":
			action := PickAction(b.config.Strategy, msg, b.config.Weapon)
			payload := map[string]interface{}{
				"type": "action",
				"tick": msg["tick"],
			}
			payload["action"] = action.Action
			if action.Target != "" {
				payload["target"] = action.Target
			}
			if action.Direction != nil {
				payload["direction"] = action.Direction
			}
			if action.TargetPosition != nil {
				payload["target_position"] = action.TargetPosition
			}
			if action.ItemID != "" {
				payload["item_id"] = action.ItemID
			}
			if err := conn.WriteJSON(payload); err != nil {
				return fmt.Errorf("send action: %w", err)
			}

		case "death":
			// Nothing to do; server handles respawn.

		case "respawn":
			// Bot is alive again.

		case "round_end":
			// Wait for next round.

		case "round_start":
			// New round started.

		case "lobby":
			// Waiting for more players.

		case "kick":
			reason, _ := msg["reason"].(string)
			b.logger.Warn("kicked", "reason", reason)
			return fmt.Errorf("kicked: %s", reason)

		case "error":
			message, _ := msg["message"].(string)
			b.logger.Warn("server error", "message", message)

		case "kill":
			// Scored a kill; no action needed.

		default:
			// Ignore unknown message types gracefully.
		}
	}
}

// readJSON reads one JSON message from the WebSocket and returns it as a map.
func readJSON(conn *websocket.Conn) (map[string]interface{}, error) {
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return msg, nil
}

// httpToWS converts an http(s) URL to a ws(s) URL.
func httpToWS(u string) string {
	if len(u) >= 8 && u[:8] == "https://" {
		return "wss://" + u[8:]
	}
	if len(u) >= 7 && u[:7] == "http://" {
		return "ws://" + u[7:]
	}
	return "ws://" + u
}
