package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/gorilla/websocket"
)

// EventHook is a callback for dashboard event logging. Set by the api package
// to avoid circular imports.
var EventHook func(action, botName, botID, ip, apiKeyID, errMsg string)

// WSMessageHook is a callback for WS message logging.
var WSMessageHook func(botID, botName, action string, data map[string]interface{})

// upgrader is the shared WebSocket upgrader for bot connections.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 65536,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

// BotHandler returns an http.HandlerFunc that manages the full lifecycle of a
// bot WebSocket connection: upgrade, authentication, loadout negotiation,
// engine registration, and the read/write message loops.
func BotHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := security.ExtractClientIP(r)

		// Check IP ban.
		if engine.IsIPBanned(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "your IP has been banned",
				"code":  "IP_BANNED",
			})
			if EventHook != nil {
				EventHook("ip_banned", "", "", ip, "", "IP banned")
			}
			return
		}

		// Per-IP WebSocket connection rate limiting (skip for localhost/demo bots).
		isLocal := ip == "::1" || ip == "127.0.0.1" || ip == "localhost"
		if config.C.WSConnectRatePerMin > 0 && !isLocal {
			allowed, count, _, err := security.CheckRateLimit(r.Context(), "ws:bot:"+ip, config.C.WSConnectRatePerMin, 60)
			if err != nil {
				slog.Warn("ws rate limit check error, allowing", "error", err, "ip", ip)
			} else if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "too many connections",
					"code":        "WS_RATE_LIMITED",
					"details": map[string]interface{}{
						"current_count": count,
						"limit":         config.C.WSConnectRatePerMin,
						"retry_after":   60,
						"window":        "60s",
					},
				})
				if EventHook != nil {
					EventHook("ws_rate_limited", "", "", ip, "", "too many connections")
				}
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
			if EventHook != nil {
				EventHook("ws_upgrade_failed", "", "", ip, "", err.Error())
			}
			return
		}

		// Ensure the connection is always closed on exit.
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic in bot handler", "recover", p)
			}
			conn.Close()
		}()

		cfg := &config.C

		// ----------------------------------------------------------------
		// 1. Authenticate
		// ----------------------------------------------------------------
		botRecord, err := authenticateBot(r, conn, cfg)
		if err != nil {
			slog.Warn("bot auth failed", "error", err, "remote", r.RemoteAddr)
			sendWSErrorStructured(conn, err.Error(), "AUTH_FAILED", map[string]interface{}{
				"ip": ip,
			})
			if EventHook != nil {
				EventHook("auth_failed", "", "", ip, "", err.Error())
			}
			return
		}

		// Check if the bot's API key is banned.
		if engine.IsKeyBanned(botRecord.APIKeyID) {
			slog.Warn("banned bot attempted reconnection", "bot", botRecord.Name, "remote", r.RemoteAddr)
			sendWSErrorStructured(conn, "your API key has been banned", "KEY_BANNED", map[string]interface{}{
				"api_key_id": botRecord.APIKeyID,
			})
			if EventHook != nil {
				EventHook("auth_banned", botRecord.Name, botRecord.ID, ip, botRecord.APIKeyID, "key banned")
			}
			return
		}

		// Per-API-key reconnect cooldown (5 second minimum between connections, skip for localhost).
		if config.C.WSConnectRatePerMin > 0 && !isLocal {
			keyRateKey := "ws:bot:key:" + botRecord.APIKeyID
			allowed, _, _, err := security.CheckRateLimit(r.Context(), keyRateKey, 1, 5)
			if err != nil {
				slog.Warn("key rate limit check error, allowing", "error", err)
			} else if !allowed {
				slog.Warn("bot reconnecting too fast", "bot", botRecord.Name, "key_id", botRecord.APIKeyID)
				sendWSErrorStructured(conn, "reconnecting too fast, wait a few seconds", "RECONNECT_TOO_FAST", map[string]interface{}{
					"retry_after": 5,
				})
				if EventHook != nil {
					EventHook("reconnect_rate_limited", botRecord.Name, botRecord.ID, ip, botRecord.APIKeyID, "reconnecting too fast")
				}
				return
			}
		}

		// ----------------------------------------------------------------
		// 2. Load bot config and stats from DB
		// ----------------------------------------------------------------
		ctx := r.Context()

		botStats, err := db.GetBotStats(ctx, botRecord.ID)
		if err != nil {
			slog.Error("failed to load bot stats", "error", err, "bot_id", botRecord.ID)
		}

		startingElo := cfg.EloStarting
		if botStats != nil {
			startingElo = botStats.Elo
		}

		// ----------------------------------------------------------------
		// 3. Create BotState
		// ----------------------------------------------------------------
		bot := &game.BotState{
			BotID:            botRecord.ID,
			APIKeyID:         botRecord.APIKeyID,
			Name:             botRecord.Name,
			AvatarColor:      botRecord.AvatarColor,
			Weapon:           botRecord.DefaultWeapon,
			Stats:            map[string]int(botRecord.DefaultStats),
			FallbackBehavior: botRecord.DefaultFallback,
			Elo:              startingElo,
			IsAlive:          false,
			ConnectedAt:      time.Now(),
			Conn:             conn,
			SendChan:         make(chan []byte, 64),
			ActiveEffects:    []game.Effect{},
			HitsReceived:     []game.HitRecord{},
		}

		// ----------------------------------------------------------------
		// 4. Send connected message (write directly — writer goroutine not started yet)
		// ----------------------------------------------------------------
		lastLoadout := map[string]interface{}{
			"weapon":            botRecord.DefaultWeapon,
			"stats":             botRecord.DefaultStats,
			"fallback_behavior": botRecord.DefaultFallback,
		}
		connMsg := game.BuildConnectedMessage(bot, lastLoadout)
		if err := conn.WriteJSON(connMsg); err != nil {
			slog.Error("failed to send connected message", "error", err, "bot", bot.Name)
			return
		}

		// ----------------------------------------------------------------
		// 5. Wait for loadout selection
		// ----------------------------------------------------------------
		if err := handleLoadoutPhase(conn, bot, cfg); err != nil {
			slog.Warn("loadout phase failed", "error", err, "bot", bot.Name)
			game.SendKick(bot, err.Error())
			if EventHook != nil {
				EventHook("loadout_failed", bot.Name, bot.BotID, ip, bot.APIKeyID, err.Error())
			}
			return
		}

		// ----------------------------------------------------------------
		// 6. Register with engine
		// ----------------------------------------------------------------
		if !engine.AddBot(bot) {
			slog.Warn("bot rejected: server at capacity", "bot", bot.Name, "remote", r.RemoteAddr)
			sendWSErrorStructured(conn, "server at capacity", "SERVER_FULL", map[string]interface{}{
				"max_bots": config.C.MaxBots,
			})
			if EventHook != nil {
				EventHook("server_full", bot.Name, bot.BotID, ip, bot.APIKeyID, "server at capacity")
			}
			return
		}

		// Log successful connection.
		if EventHook != nil {
			EventHook("connected", bot.Name, bot.BotID, ip, bot.APIKeyID, "")
		}

		// ----------------------------------------------------------------
		// 7. Start read/write goroutines
		// ----------------------------------------------------------------
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var wg sync.WaitGroup

		// Writer goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			botWriter(runCtx, conn, bot.SendChan)
		}()

		// Reader loop (runs on this goroutine)
		botReader(runCtx, cancel, conn, bot, engine, cfg)

		// ----------------------------------------------------------------
		// 8. Cleanup on disconnect
		// ----------------------------------------------------------------
		cancel()
		engine.RemoveBot(bot.BotID)
		close(bot.SendChan)

		wg.Wait()
		slog.Info("bot disconnected", "bot", bot.Name, "bot_id", bot.BotID)
		if EventHook != nil {
			EventHook("disconnected", bot.Name, bot.BotID, ip, bot.APIKeyID, "")
		}
	}
}

// authenticateBot extracts an API key from the request (query param, header,
// or initial auth message) and verifies it against the database. Returns the
// associated bot record on success.
func authenticateBot(r *http.Request, conn *websocket.Conn, cfg *config.Config) (*db.Bot, error) {
	ctx := r.Context()

	// Try query parameter first.
	apiKey := r.URL.Query().Get("key")

	// Try X-Arena-Key header.
	if apiKey == "" {
		apiKey = r.Header.Get("X-Arena-Key")
	}

	// Fall back to reading an auth message from the WebSocket.
	if apiKey == "" {
		deadline := time.Now().Add(time.Duration(cfg.ConnectionTimeout * float64(time.Second)))
		conn.SetReadDeadline(deadline)

		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("failed to read auth message: %w", err)
		}

		msgType, msg, err := ParseBotMessage(data)
		if err != nil || msgType != "auth" {
			return nil, fmt.Errorf("expected auth message, got %q", msgType)
		}
		authMsg, ok := msg.(*AuthMessage)
		if !ok || authMsg.APIKey == "" {
			return nil, fmt.Errorf("missing api_key in auth message")
		}
		apiKey = authMsg.APIKey

		// Clear the read deadline.
		conn.SetReadDeadline(time.Time{})
	}

	bot, err := security.VerifyAPIKey(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return bot, nil
}

// handleLoadoutPhase reads a loadout selection from the bot, validates it,
// applies it to the BotState, and sends a loadout_confirmed response.
// If the bot sends an invalid loadout, defaults are used instead.
func handleLoadoutPhase(conn *websocket.Conn, bot *game.BotState, cfg *config.Config) error {
	deadline := time.Now().Add(time.Duration(cfg.LoadoutTimeoutSecs * float64(time.Second)))
	conn.SetReadDeadline(deadline)
	defer conn.SetReadDeadline(time.Time{})

	// Helper to write directly to conn (writer goroutine not started yet).
	writeMsg := func(msg interface{}) {
		if err := conn.WriteJSON(msg); err != nil {
			slog.Warn("direct write failed in loadout phase", "error", err, "bot", bot.Name)
		}
	}
	sendError := func(message string) {
		writeMsg(map[string]string{"type": "error", "message": message})
	}
	sendConfirmed := func() {
		derived := game.ComputeDerivedStats(bot.Stats, bot.Weapon)
		writeMsg(game.BuildLoadoutConfirmed(bot, derived))
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		// Timeout or read error -- use defaults, which are already set.
		slog.Warn("loadout read failed, using defaults", "error", err, "bot", bot.Name)
		applyDerivedStats(bot)
		sendConfirmed()
		return nil
	}

	msgType, msg, err := ParseBotMessage(data)
	if err != nil || msgType != "select_loadout" {
		slog.Warn("expected select_loadout, got something else, using defaults", "bot", bot.Name, "type", msgType)
		applyDerivedStats(bot)
		sendConfirmed()
		return nil
	}

	loadout, ok := msg.(*LoadoutMessage)
	if !ok {
		applyDerivedStats(bot)
		sendConfirmed()
		return nil
	}

	// Validate and apply weapon.
	if security.ValidateWeapon(loadout.Weapon) {
		bot.Weapon = loadout.Weapon
	} else {
		sendError(fmt.Sprintf("invalid weapon %q, using default %q", loadout.Weapon, bot.Weapon))
	}

	// Validate and apply stats.
	if err := security.ValidateStats(loadout.Stats); err != nil {
		sendError(fmt.Sprintf("invalid stats: %s, using defaults", err.Error()))
	} else {
		bot.Stats = loadout.Stats
	}

	// Validate and apply fallback behavior.
	if security.ValidateFallbackBehavior(loadout.Fallback) {
		bot.FallbackBehavior = loadout.Fallback
	} else {
		sendError(fmt.Sprintf("invalid fallback %q, using default %q", loadout.Fallback, bot.FallbackBehavior))
	}

	// Compute and apply derived stats.
	applyDerivedStats(bot)
	sendConfirmed()

	return nil
}

// applyDerivedStats computes derived stats from the bot's current stat
// allocation and weapon, then applies them to the BotState fields.
func applyDerivedStats(bot *game.BotState) {
	derived := game.ComputeDerivedStats(bot.Stats, bot.Weapon)
	bot.MaxHP = derived.MaxHP
	bot.HP = derived.MaxHP
	bot.Speed = derived.MoveSpeed
	bot.AttackMultiplier = derived.AttackMult
	bot.DefenseReduction = derived.DefenseReduction
}

// botReader is the main read loop for a connected bot. It reads messages from
// the WebSocket, rate-limits them, parses actions, and forwards them to the
// game engine. It returns when the connection is closed or an error occurs.
func botReader(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, bot *game.BotState, engine *game.GameEngine, cfg *config.Config) {
	defer cancel()

	conn.SetReadLimit(int64(cfg.WSMessageMaxBytes))

	// Rate limiting state: sliding window of message timestamps.
	var msgTimestamps []time.Time
	maxPerSec := cfg.WSMaxMessagesPerSec

	// Set initial read deadline for heartbeat detection.
	heartbeatTimeout := time.Duration(cfg.HeartbeatInterval*float64(time.Second)) * 2
	conn.SetReadDeadline(time.Now().Add(heartbeatTimeout))

	// Handle WebSocket pong messages to reset the read deadline.
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(heartbeatTimeout))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("bot read error", "error", err, "bot", bot.Name)
			}
			return
		}

		// Reset read deadline on any received message.
		conn.SetReadDeadline(time.Now().Add(heartbeatTimeout))

		// Rate limiting: prune old timestamps and check rate.
		now := time.Now()
		cutoff := now.Add(-time.Second)
		filtered := msgTimestamps[:0]
		for _, t := range msgTimestamps {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		msgTimestamps = filtered

		if len(msgTimestamps) >= maxPerSec {
			slog.Warn("rate limited bot", "bot", bot.Name, "msgs_per_sec", len(msgTimestamps))
			game.SendStructuredError(bot, "Rate limited: too many messages per second", "WS_RATE_LIMITED", map[string]interface{}{
				"current_count": len(msgTimestamps),
				"limit":         maxPerSec,
				"window":        "1s",
			})
			continue
		}
		msgTimestamps = append(msgTimestamps, now)

		// Parse the message.
		msgType, msg, err := ParseBotMessage(data)
		if err != nil {
			game.SendError(bot, "Invalid message")
			continue
		}

		switch msgType {
		case "action":
			actionMsg, ok := msg.(*ActionMessage)
			if !ok {
				continue
			}
			action := ActionMessageToAction(actionMsg)
			engine.SetBotAction(bot.BotID, action)

			// Log WS message for dashboard.
			if WSMessageHook != nil {
				WSMessageHook(bot.BotID, bot.Name, actionMsg.Action, map[string]interface{}{
					"tick":   actionMsg.Tick,
					"target": actionMsg.Target,
				})
			}

		case "select_loadout":
			// Loadout changes are only allowed during the initial phase.
			game.SendStructuredError(bot, "Cannot change loadout mid-game", "LOADOUT_LOCKED", nil)

		default:
			game.SendStructuredError(bot, "Unexpected message type: "+msgType, "UNKNOWN_MSG_TYPE", map[string]interface{}{
				"received_type": msgType,
			})
		}
	}
}

// botWriter drains the bot's SendChan and writes each message to the
// WebSocket connection. It returns when the context is cancelled or the
// send channel is closed.
func botWriter(ctx context.Context, conn *websocket.Conn, sendChan <-chan []byte) {
	// Ping every 20 seconds to keep the connection alive through proxies.
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return

		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Warn("bot ping error", "error", err)
				return
			}

		case msg, ok := <-sendChan:
			if !ok {
				conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}

			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("bot write error", "error", err)
				return
			}
		}
	}
}

// sendWSError writes a JSON error message directly to a WebSocket connection.
// This is used before a BotState exists (e.g., during authentication).
func sendWSError(conn *websocket.Conn, message string) {
	payload, _ := json.Marshal(map[string]string{
		"type":    "error",
		"message": message,
	})
	conn.WriteMessage(websocket.TextMessage, payload)
}

// sendWSErrorStructured writes a structured JSON error with code and details
// to a WebSocket connection before closing it.
func sendWSErrorStructured(conn *websocket.Conn, message, code string, details map[string]interface{}) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type":    "error",
		"message": message,
		"code":    code,
		"details": details,
	})
	conn.WriteMessage(websocket.TextMessage, payload)
}
