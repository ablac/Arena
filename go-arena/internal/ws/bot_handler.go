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

// upgrader is the shared WebSocket upgrader for bot connections.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

// BotHandler returns an http.HandlerFunc that manages the full lifecycle of a
// bot WebSocket connection: upgrade, authentication, loadout negotiation,
// engine registration, and the read/write message loops.
func BotHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Per-IP WebSocket connection rate limiting.
		if config.C.WSConnectRatePerMin > 0 {
			ip := security.ExtractClientIP(r)
			allowed, _, _, err := security.CheckRateLimit(r.Context(), "ws:bot:"+ip, config.C.WSConnectRatePerMin, 60)
			if err != nil {
				slog.Warn("ws rate limit check error, allowing", "error", err, "ip", ip)
			} else if !allowed {
				http.Error(w, "too many connections", http.StatusTooManyRequests)
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
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
			sendWSError(conn, err.Error())
			return
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
			return
		}

		// ----------------------------------------------------------------
		// 6. Register with engine
		// ----------------------------------------------------------------
		if !engine.AddBot(bot) {
			slog.Warn("bot rejected: server at capacity", "bot", bot.Name, "remote", r.RemoteAddr)
			sendWSError(conn, "server at capacity")
			return
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
			game.SendError(bot, "Rate limited")
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

		case "select_loadout":
			// Loadout changes are only allowed during the initial phase.
			game.SendError(bot, "Cannot change loadout mid-game")

		default:
			game.SendError(bot, "Unexpected message type")
		}
	}
}

// botWriter drains the bot's SendChan and writes each message to the
// WebSocket connection. It returns when the context is cancelled or the
// send channel is closed.
func botWriter(ctx context.Context, conn *websocket.Conn, sendChan <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			// Write a close message before returning.
			conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return

		case msg, ok := <-sendChan:
			if !ok {
				// Channel closed.
				conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				)
				return
			}

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
