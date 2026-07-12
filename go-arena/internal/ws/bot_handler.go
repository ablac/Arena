package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
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

type botCosmeticsLoader func(context.Context, string) (map[string]string, error)

// refreshAdmittedBotCosmetics closes the gap between the pre-admission DB read
// and AddBot. A terminal payment reversal can commit while loadout negotiation
// is in progress, before the bot is visible to the commerce refresh snapshot.
// Re-reading after admission guarantees either this read or that snapshot sees
// the committed state. On a read failure, clear to standard visuals rather than
// retain a potentially revoked paid loadout.
func refreshAdmittedBotCosmetics(ctx context.Context, engine *game.GameEngine, botID string, load botCosmeticsLoader) (bool, error) {
	cosmetics, err := load(ctx, botID)
	if err != nil {
		cosmetics = map[string]string{}
	}
	return engine.UpdateBotCosmetics(botID, cosmetics), err
}

// upgrader is the shared WebSocket upgrader for bot connections.
var upgrader = websocket.Upgrader{
	ReadBufferSize:    4096,
	WriteBufferSize:   65536,
	EnableCompression: true,
	CheckOrigin: func(r *http.Request) bool {
		return true // allow all origins for now
	},
}

func botUpgradeRateLimit(ip string) (string, int) {
	// Standard WebSocket clients authenticate in their first message, so the
	// HTTP upgrade cannot distinguish them from an anonymous connection. Use
	// one non-bypassable, bounded per-IP burst bucket for bots behind one NAT;
	// authenticated sessions still face the stricter per-key cooldown.
	limit := max(config.C.WSConnectRatePerMin, 40)
	return "ws:bot:connect:" + ip, limit
}

func botKeyConnectRateLimit(botID, apiKeyID string, resume bool) (key string, limit, windowSecs int) {
	if resume {
		// Recovery traffic has its own small burst bucket, so the initial
		// admission does not make the first one-second SDK retry fail. The bound
		// still stops a broken or malicious client from thrashing sessions.
		return "ws:bot:resume:" + botID + ":" + apiKeyID, 3, 5
	}
	return "ws:bot:key:" + apiKeyID, 1, 5
}

// BotHandler returns an http.HandlerFunc that manages the full lifecycle of a
// bot WebSocket connection: upgrade, authentication, loadout negotiation,
// engine registration, and the read/write message loops.
func BotHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := security.ExtractClientIP(r)
		remoteAddr := r.RemoteAddr

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
		// One bounded pre-auth bucket supports documented message authentication
		// without letting clients bypass the limit by changing auth transport.
		isLocal := ip == "::1" || ip == "127.0.0.1" || ip == "localhost"
		if config.C.WSConnectRatePerMin > 0 && !isLocal {
			limitKey, limit := botUpgradeRateLimit(ip)

			allowed, count, _, err := security.CheckRateLimit(r.Context(), limitKey, limit, 60)
			if err != nil {
				slog.Warn("ws rate limit check error, allowing", "error", err, "ip", ip)
			} else if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "too many connections",
					"code":  "WS_RATE_LIMITED",
					"details": map[string]interface{}{
						"current_count": count,
						"limit":         limit,
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

		// If the client already presented a key in the HTTP upgrade request,
		// validate it before upgrading so invalid keys fail as plain HTTP 401s
		// instead of noisy websocket auth errors.
		if presentedKey := presentedAPIKey(r); presentedKey != "" {
			if _, err := security.VerifyAPIKey(r.Context(), presentedKey); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": classifyAPIKeyError(err.Error()),
					"code":  "INVALID_API_KEY",
				})
				if EventHook != nil {
					EventHook("auth_failed", "", "", ip, "", err.Error())
				}
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket upgrade failed", "error", err, "client_ip", ip, "remote", remoteAddr)
			if EventHook != nil {
				EventHook("ws_upgrade_failed", "", "", ip, "", err.Error())
			}
			return
		}
		cfg := &config.C
		// Bound every client-controlled frame from the first WebSocket read,
		// including authentication and loadout negotiation.
		conn.SetReadLimit(int64(cfg.WSMessageMaxBytes))

		// Ensure the connection is always closed on exit.
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic in bot handler", "recover", p)
			}
			conn.Close()
		}()

		// ----------------------------------------------------------------
		// 1. Authenticate
		// ----------------------------------------------------------------
		botRecord, err := authenticateBot(r, conn, cfg)
		if err != nil {
			slog.Warn("bot auth failed", "error", err, "client_ip", ip, "remote", remoteAddr)
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
			slog.Warn("banned bot attempted reconnection", "bot", botRecord.Name, "client_ip", ip, "remote", remoteAddr)
			sendWSErrorStructured(conn, "your API key has been banned", "KEY_BANNED", map[string]interface{}{
				"api_key_id": botRecord.APIKeyID,
			})
			if EventHook != nil {
				EventHook("auth_banned", botRecord.Name, botRecord.ID, ip, botRecord.APIKeyID, "key banned")
			}
			return
		}
		if rejectTemporarilyLockedBot(conn, botRecord.Name, botRecord.ID, ip, botRecord.APIKeyID) {
			return
		}

		// Per-key admission/recovery limiting (skip for localhost demo bots).
		if config.C.WSConnectRatePerMin > 0 && !isLocal {
			resume := engine.HasBotSessionForKey(botRecord.ID, botRecord.APIKeyID)
			keyRateKey, limit, windowSecs := botKeyConnectRateLimit(botRecord.ID, botRecord.APIKeyID, resume)
			allowed, _, _, err := security.CheckRateLimit(r.Context(), keyRateKey, limit, windowSecs)
			if err != nil {
				slog.Warn("key rate limit check error, allowing", "error", err)
			} else if !allowed {
				slog.Warn("bot reconnecting too fast", "bot", botRecord.Name, "key_id", botRecord.APIKeyID)
				sendWSErrorStructured(conn, "reconnecting too fast, wait a few seconds", "RECONNECT_TOO_FAST", map[string]interface{}{
					"retry_after": windowSecs,
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
		if normalizeStoredLoadout(botRecord) {
			botRecord.UpdatedAt = time.Now()
			if err := db.UpdateBot(ctx, botRecord); err != nil {
				slog.Warn("failed to persist normalized stored loadout; using safe runtime values", "error", err, "bot_id", botRecord.ID)
			} else {
				slog.Warn("normalized invalid historical bot loadout", "bot_id", botRecord.ID)
			}
		}

		botStats, err := db.GetBotStats(ctx, botRecord.ID)
		if err != nil {
			slog.Error("failed to load bot stats", "error", err, "bot_id", botRecord.ID)
		}

		startingElo := config.StartingElo()
		if botStats != nil {
			startingElo = botStats.Elo
		}
		startingElo = game.ClampElo(startingElo)
		cosmetics, err := db.GetEquippedCosmetics(ctx, botRecord.ID)
		if err != nil {
			slog.Warn("failed to load bot cosmetics; using standard visuals", "error", err, "bot_id", botRecord.ID)
			cosmetics = map[string]string{}
		}

		// ----------------------------------------------------------------
		// 3. Create BotState
		// ----------------------------------------------------------------
		bot := &game.BotState{
			BotID:            botRecord.ID,
			APIKeyID:         botRecord.APIKeyID,
			Name:             botRecord.Name,
			AvatarColor:      botRecord.AvatarColor,
			Cosmetics:        cosmetics,
			Weapon:           botRecord.DefaultWeapon,
			Stats:            cloneStats(map[string]int(botRecord.DefaultStats)),
			FallbackBehavior: botRecord.DefaultFallback,
			Elo:              startingElo,
			IsAlive:          false,
			ConnectedAt:      time.Now(),
			Conn:             conn,
			SendChan:         make(chan []byte, 64),
			TickChan:         make(chan []byte, 1),
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
		connMsg["service_status"] = engine.GetServiceStatus()
		if err := conn.WriteJSON(connMsg); err != nil {
			slog.Error("failed to send connected message", "error", err, "bot", bot.Name)
			return
		}

		// ----------------------------------------------------------------
		// 5. Wait for loadout selection
		// ----------------------------------------------------------------
		if err := handleLoadoutPhase(conn, bot, cfg); err != nil {
			slog.Warn("loadout phase failed", "error", err, "bot", bot.Name)
			var violation *clientViolationError
			if errors.As(err, &violation) {
				result := botViolationTracker.Record(bot.APIKeyID)
				message, code, details := violationResponse(violation.Message, violation.Code, violation.Details, result)
				sendWSErrorStructured(conn, message, code, details)
				disconnectProtocolLockedSession(engine, bot, result)
			} else {
				sendWSErrorStructured(conn, "loadout negotiation failed", "LOADOUT_FAILED", nil)
			}
			if EventHook != nil {
				EventHook("loadout_failed", bot.Name, bot.BotID, ip, bot.APIKeyID, err.Error())
			}
			return
		}

		// ----------------------------------------------------------------
		// 6. Register with engine
		// ----------------------------------------------------------------
		// A different session sharing this key may have crossed the strike limit
		// while this connection was in loadout negotiation. Recheck at the actual
		// admission boundary so an already-locked key cannot wait its way in.
		if rejectPermanentlyBannedBot(engine, conn, bot.Name, bot.BotID, ip, bot.APIKeyID) {
			return
		}
		if rejectTemporarilyLockedBot(conn, bot.Name, bot.BotID, ip, bot.APIKeyID) {
			return
		}
		if !engine.AddBot(bot) {
			slog.Warn("bot rejected: server at capacity", "bot", bot.Name, "client_ip", ip, "remote", remoteAddr)
			sendWSErrorStructured(conn, "server at capacity", "SERVER_FULL", map[string]interface{}{
				"max_bots": config.C.MaxBots,
			})
			if EventHook != nil {
				EventHook("server_full", bot.Name, bot.BotID, ip, bot.APIKeyID, "server at capacity")
			}
			return
		}
		current, refreshErr := refreshAdmittedBotCosmetics(ctx, engine, bot.BotID, db.GetEquippedCosmetics)
		if refreshErr != nil {
			slog.Warn("failed to refresh cosmetics at bot admission; using standard visuals", "error", refreshErr, "bot_id", bot.BotID)
		}
		if !current {
			return
		}
		// Close the final check/AddBot interleaving window: if a ban or protocol
		// lock landed while AddBot waited for the engine lock, roll back only this
		// exact admitted session. RemoveBot is pointer-checked against reconnects.
		if rejectPermanentlyBannedBot(engine, conn, bot.Name, bot.BotID, ip, bot.APIKeyID) ||
			rejectTemporarilyLockedBot(conn, bot.Name, bot.BotID, ip, bot.APIKeyID) {
			engine.RemoveBot(bot.BotID, bot)
			return
		}
		// Admission may replace this session's requested loadout with the
		// authoritative round-locked loadout. Confirm only after that decision so
		// clients receive exactly one, authoritative confirmation.
		confirmation, current := engine.BuildLoadoutConfirmationForSession(bot.BotID, bot)
		if !current {
			return
		}
		if err := conn.WriteJSON(confirmation); err != nil {
			slog.Error("failed to send admitted loadout confirmation", "error", err, "bot", bot.Name)
			engine.RemoveBot(bot.BotID, bot)
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
			botWriter(runCtx, conn, bot.SendChan, bot.TickChan)
			// A write or ping failure must wake the reader immediately; otherwise
			// the engine can retain a frozen half-open session until heartbeat expiry.
			cancel()
			_ = conn.Close()
		}()

		// Reader loop (runs on this goroutine)
		botReader(runCtx, cancel, conn, bot, engine, cfg)

		// ----------------------------------------------------------------
		// 8. Cleanup on disconnect
		// ----------------------------------------------------------------
		sendChan := bot.SendChan
		tickChan := bot.TickChan
		cancel()
		preserved := engine.DetachBotSession(bot.BotID, bot)
		if !preserved {
			engine.RemoveBot(bot.BotID, bot)
		}
		if sendChan != nil {
			close(sendChan)
		}
		if tickChan != nil {
			close(tickChan)
		}

		wg.Wait()
		slog.Info("bot disconnected", "bot", bot.Name, "bot_id", bot.BotID, "reconnect_preserved", preserved)
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
	apiKey := presentedAPIKey(r)

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

func presentedAPIKey(r *http.Request) string {
	if apiKey := r.URL.Query().Get("key"); apiKey != "" {
		return apiKey
	}
	return r.Header.Get("X-Arena-Key")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func classifyAPIKeyError(errMsg string) string {
	switch {
	case errMsg == "":
		return "invalid API key"
	case containsAny(errMsg, "too short"):
		return "API key is too short"
	case containsAny(errMsg, "not found"):
		return "API key not found"
	case containsAny(errMsg, "no bot associated"):
		return "no bot associated with this API key"
	default:
		return "invalid API key"
	}
}

func containsAny(s string, patterns ...string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(s, pattern) {
			return true
		}
	}
	return false
}

// handleLoadoutPhase reads a loadout selection from the bot, validates it,
// and applies it to the BotState. The caller confirms the authoritative
// loadout only after engine admission has resolved reconnect state.
// Invalid client-supplied loadouts are rejected atomically. A read timeout is
// terminal because Gorilla WebSocket connections cannot safely resume reads
// after a read deadline fires; the client can reconnect and negotiate again.
func handleLoadoutPhase(conn *websocket.Conn, bot *game.BotState, cfg *config.Config) error {
	// Keep this read boundary safe when exercised independently in tests or by
	// future callers; BotHandler installs the same limit immediately on upgrade.
	conn.SetReadLimit(int64(cfg.WSMessageMaxBytes))
	deadline := time.Now().Add(time.Duration(cfg.LoadoutTimeoutSecs * float64(time.Second)))
	conn.SetReadDeadline(deadline)
	defer conn.SetReadDeadline(time.Time{})

	_, data, err := conn.ReadMessage()
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("loadout selection timed out: %w", err)
		}
		return fmt.Errorf("loadout read failed: %w", err)
	}

	msgType, msg, err := ParseBotMessage(data)
	if err != nil {
		return newClientViolation("INVALID_LOADOUT_MESSAGE", "invalid loadout message", map[string]interface{}{"error": err.Error()})
	}
	if msgType != "select_loadout" {
		return newClientViolation("EXPECTED_LOADOUT", "expected select_loadout message", map[string]interface{}{"received_type": msgType})
	}

	loadout, ok := msg.(*LoadoutMessage)
	if !ok {
		return newClientViolation("INVALID_LOADOUT_MESSAGE", "invalid loadout message", nil)
	}
	if err := applySelectedLoadout(bot, loadout); err != nil {
		return newClientViolation("INVALID_LOADOUT", "loadout rejected", map[string]interface{}{"error": err.Error()})
	}

	// Compute and apply derived stats.
	applyDerivedStats(bot)

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

	messageLimiter := newBotMessageLimiter(cfg.WSMaxMessagesPerSec)

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

		decision := messageLimiter.Check(time.Now())
		if !decision.Allowed {
			details := map[string]interface{}{
				"current_count": decision.CurrentCount,
				"dropped_count": decision.DroppedCount,
				"limit":         cfg.WSMaxMessagesPerSec,
				"window":        "1s",
			}
			if decision.Notify {
				slog.Warn("transient bot message burst", "bot", bot.Name, "msgs_per_sec", decision.CurrentCount)
				game.SendStructuredError(bot, "Rate limited: too many messages per second", "WS_RATE_LIMITED", details)
			}
			if decision.Punish {
				slog.Warn("sustained bot message flood", "bot", bot.Name, "dropped", decision.DroppedCount)
				if rejectBotViolation(engine, bot, "Sustained message flood", "WS_RATE_LIMITED", details) {
					closeForProtocolViolation(conn)
					return
				}
			}
			continue
		}

		// Parse the message.
		msgType, msg, err := ParseBotMessage(data)
		if err != nil {
			if rejectBotViolation(engine, bot, "Invalid message", "INVALID_MESSAGE", map[string]interface{}{"error": err.Error()}) {
				closeForProtocolViolation(conn)
				return
			}
			continue
		}

		switch msgType {
		case "action":
			actionMsg, ok := msg.(*ActionMessage)
			if !ok {
				if rejectBotViolation(engine, bot, "Invalid action message", "INVALID_ACTION", nil) {
					closeForProtocolViolation(conn)
					return
				}
				continue
			}
			action, err := prepareActionForBot(bot, actionMsg)
			if err != nil {
				if rejectBotViolation(engine, bot, "Action rejected", "INVALID_ACTION", map[string]interface{}{"error": err.Error()}) {
					closeForProtocolViolation(conn)
					return
				}
				continue
			}
			if err := engine.SubmitBotActionForSession(bot.BotID, bot, actionMsg.Tick, action); err != nil {
				if errors.Is(err, game.ErrActionSessionReplaced) {
					return
				}
				if sendActionSubmissionError(engine, bot, actionMsg.Tick, err) {
					closeForProtocolViolation(conn)
					return
				}
				continue
			}

			// Log WS message for dashboard.
			if WSMessageHook != nil {
				WSMessageHook(bot.BotID, bot.Name, actionMsg.Action, map[string]interface{}{
					"tick":   actionMsg.Tick,
					"target": actionMsg.Target,
				})
			}

		case "select_loadout":
			// Loadout changes are only allowed during the initial phase.
			if rejectBotViolation(engine, bot, "Cannot change loadout mid-game", "LOADOUT_LOCKED", nil) {
				closeForProtocolViolation(conn)
				return
			}

		case "taunt":
			tauntMsg, ok := msg.(*TauntMessage)
			if !ok {
				continue
			}
			// Taunts are cosmetic: every rejection except an unknown emote
			// is a silent drop, and none of them accrue violation strikes
			// (a cooldown-spamming taunter is enthusiastic, not malicious).
			if err := engine.AddTauntForSession(bot.BotID, bot, tauntMsg.Emote); err != nil {
				if errors.Is(err, game.ErrTauntInvalidEmote) {
					game.SendStructuredError(bot, "Unknown taunt emote", "TAUNT_INVALID_EMOTE", map[string]interface{}{
						"emote":  tauntMsg.Emote,
						"emotes": game.TauntEmoteKeys(),
					})
				}
				continue
			}

		default:
			if rejectBotViolation(engine, bot, "Unexpected message type: "+msgType, "UNKNOWN_MSG_TYPE", map[string]interface{}{
				"received_type": msgType,
			}) {
				closeForProtocolViolation(conn)
				return
			}
		}
	}
}

func rejectBotViolation(engine *game.GameEngine, bot *game.BotState, message, code string, details map[string]interface{}) bool {
	violationCode := code
	result := botViolationTracker.Record(bot.APIKeyID)
	slog.Warn("bot protocol violation",
		"bot", bot.Name,
		"bot_id", bot.BotID,
		"code", violationCode,
		"strikes", result.Strikes,
		"locked", result.Locked,
	)
	message, code, details = violationResponse(message, code, details, result)
	game.SendStructuredError(bot, message, code, details)
	disconnectProtocolLockedSession(engine, bot, result)
	return result.Locked
}

func violationResponse(message, code string, details map[string]interface{}, result violationResult) (string, string, map[string]interface{}) {
	responseDetails := make(map[string]interface{}, len(details)+3)
	for key, value := range details {
		responseDetails[key] = value
	}
	responseDetails["strikes"] = result.Strikes
	if result.Locked {
		message = "API key temporarily locked after repeated protocol violations"
		code = "API_KEY_TEMP_LOCKED"
		responseDetails["retry_after"] = durationSecondsCeil(result.RetryAfter)
	} else {
		remaining := botViolationTracker.strikeLimit - result.Strikes
		if remaining < 0 {
			remaining = 0
		}
		responseDetails["strikes_remaining"] = remaining
	}
	return message, code, responseDetails
}

func durationSecondsCeil(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int((duration + time.Second - 1) / time.Second)
}

func rejectTemporarilyLockedBot(conn *websocket.Conn, botName, botID, ip, apiKeyID string) bool {
	retrySeconds, locked := temporaryProtocolLockRetrySeconds(apiKeyID)
	if !locked {
		return false
	}
	sendWSErrorStructured(conn, "API key temporarily locked after repeated protocol violations", "API_KEY_TEMP_LOCKED", map[string]interface{}{
		"retry_after": retrySeconds,
	})
	if EventHook != nil {
		EventHook("protocol_temp_locked", botName, botID, ip, apiKeyID, "temporary protocol lock")
	}
	return true
}

func rejectPermanentlyBannedBot(engine *game.GameEngine, conn *websocket.Conn, botName, botID, ip, apiKeyID string) bool {
	if engine.IsIPBanned(ip) {
		sendWSErrorStructured(conn, "your IP has been banned", "IP_BANNED", nil)
		if EventHook != nil {
			EventHook("ip_banned", botName, botID, ip, apiKeyID, "IP banned")
		}
		return true
	}
	if engine.IsKeyBanned(apiKeyID) {
		sendWSErrorStructured(conn, "your API key has been banned", "KEY_BANNED", map[string]interface{}{
			"api_key_id": apiKeyID,
		})
		if EventHook != nil {
			EventHook("auth_banned", botName, botID, ip, apiKeyID, "key banned")
		}
		return true
	}
	return false
}

func temporaryProtocolLockRetrySeconds(apiKeyID string) (int, bool) {
	retryAfter, locked := botViolationTracker.IsLocked(apiKeyID)
	return durationSecondsCeil(retryAfter), locked
}

func disconnectProtocolLockedSession(engine *game.GameEngine, bot *game.BotState, result violationResult) bool {
	if !result.Locked || engine == nil || bot == nil {
		return false
	}
	return engine.DisconnectBotSessionForKey(bot.BotID, bot.APIKeyID)
}

func sendActionSubmissionError(engine *game.GameEngine, bot *game.BotState, clientTick int, err error) bool {
	message, code, punitive := classifyActionSubmissionError(err)
	details := map[string]interface{}{"client_tick": clientTick}
	if !punitive {
		game.SendStructuredError(bot, message, code, details)
		return false
	}
	return rejectBotViolation(engine, bot, message, code, details)
}

func classifyActionSubmissionError(err error) (string, string, bool) {
	switch {
	case errors.Is(err, game.ErrActionTickDuplicate):
		return "Duplicate action tick rejected", "DUPLICATE_ACTION_TICK", false
	case errors.Is(err, game.ErrActionServerTickUsed):
		return "An action was already accepted this server tick", "SERVER_TICK_ACTION_LOCKED", false
	case errors.Is(err, game.ErrActionTickStale):
		return "Stale action tick rejected", "STALE_ACTION_TICK", false
	case errors.Is(err, game.ErrActionTargetNotVisible):
		return "Target is outside current visibility", "TARGET_NOT_VISIBLE", false
	case errors.Is(err, game.ErrActionRoundNotActive):
		return "Round is not active", "ROUND_NOT_ACTIVE", false
	case errors.Is(err, game.ErrActionBotNotAlive):
		return "Bot is not alive", "BOT_NOT_ALIVE", false
	case errors.Is(err, game.ErrActionTickFuture):
		return "Future action tick rejected", "FUTURE_ACTION_TICK", true
	case errors.Is(err, game.ErrActionBotNotFound):
		return "Bot is not active", "BOT_NOT_ACTIVE", true
	default:
		return "Action rejected", "INVALID_ACTION", true
	}
}

func closeForProtocolViolation(conn *websocket.Conn) {
	deadline := time.Now().Add(time.Second)
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "temporary protocol lock"),
		deadline,
	)
}

// botWriter drains the bot's SendChan and writes each message to the
// WebSocket connection. It returns when the context is cancelled or the
// send channel is closed.
func botWriter(ctx context.Context, conn *websocket.Conn, sendChan, tickChan <-chan []byte) {
	// Ping every 20 seconds to keep the connection alive through proxies.
	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return
		default:
		}

		// Preserve lifecycle ordering when both queues are ready: round/death/
		// error control messages were enqueued before the replaceable snapshot
		// that follows them and must reach the client first.
		select {
		case msg, ok := <-sendChan:
			if !ok {
				return
			}
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("bot write error", "error", err)
				return
			}
			continue
		default:
		}

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

		case msg, ok := <-tickChan:
			if !ok {
				return
			}

			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Warn("bot tick write error", "error", err)
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
