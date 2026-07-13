package demobots

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/security"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type demoBotCredentialProvisioner func(context.Context, BotConfig) (string, error)
type demoBotCosmeticProvisioner func(context.Context, string, BotConfig) ([]cosmeticSelection, error)

// demoBot represents a single demo bot client that connects to the arena
// server via REST + WebSocket, exactly like a real SDK bot.
type demoBot struct {
	config                BotConfig
	serverURL             string // e.g. "http://localhost:8000"
	apiKey                string
	logger                *slog.Logger
	client                *http.Client
	attackRange           int     // Chebyshev grid range from loadout_confirmed
	maxHP                 float64 // max HP from loadout_confirmed
	botID                 string  // bot ID from connected message
	strategy              string  // stable configured archetype for comparable balance samples
	credentialProvisioner demoBotCredentialProvisioner
	cosmeticProvisioner   demoBotCosmeticProvisioner
}

// newDemoBot creates a demoBot from a config and server URL.
func newDemoBot(cfg BotConfig, serverURL string) *demoBot {
	initialStrategy := configuredStrategy(cfg.Weapon, cfg.Strategy)
	return &demoBot{
		config:                cfg,
		serverURL:             serverURL,
		logger:                slog.With("demo_bot", cfg.Name),
		strategy:              initialStrategy,
		credentialProvisioner: provisionDemoBotCredential,
		cosmeticProvisioner:   provisionDemoBotCosmetics,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func provisionDemoBotCosmetics(ctx context.Context, botID string, cfg BotConfig) ([]cosmeticSelection, error) {
	if cfg.CosmeticPackID == "" {
		return nil, nil
	}
	if botID == "" {
		return nil, errors.New("demo bot ID is unavailable")
	}
	catalog, err := db.GetPublicCosmeticCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("load public cosmetic catalog: %w", err)
	}
	selections, err := cosmeticSelectionsForPack(*catalog, cfg.CosmeticPackID)
	if err != nil {
		return nil, err
	}
	for _, selection := range selections {
		if _, err := db.GrantCosmeticEntitlement(ctx, botID, selection.CosmeticID, "demo", ""); err != nil {
			return nil, fmt.Errorf("grant %s: %w", selection.CosmeticID, err)
		}
	}
	for _, selection := range selections {
		if _, err := db.EquipCosmetic(ctx, botID, selection.Slot, selection.CosmeticID); err != nil {
			return nil, fmt.Errorf("equip %s: %w", selection.CosmeticID, err)
		}
	}
	return selections, nil
}

func provisionDemoBotCredential(ctx context.Context, cfg BotConfig) (string, error) {
	fullKey, keyHash, keyPrefix, err := security.GenerateAPIKey()
	if err != nil {
		return "", err
	}
	keyID := uuid.NewString()
	now := time.Now()
	bot := &db.Bot{
		ID:              uuid.NewString(),
		APIKeyID:        keyID,
		Name:            security.SanitizeBotName(cfg.Name),
		AvatarColor:     "#888888",
		DefaultWeapon:   "sword",
		DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.CreateAPIKeyAndBot(ctx, keyID, keyHash, keyPrefix, "127.0.0.1", bot); err != nil {
		return "", err
	}
	return fullKey, nil
}

// register either reuses a persisted API key or generates a new one,
// then configures the bot name and avatar.
func (b *demoBot) register(ctx context.Context) error {
	reused := false
	// Try to load an existing key from the database.
	if db.Pool != nil {
		existing, err := db.GetDemoBotKey(ctx, b.config.Name)
		if err == nil && existing != "" {
			// Verify the key still works by attempting a config call.
			b.apiKey = existing
			if err := b.configure(ctx); err == nil {
				b.logger.Info("reusing persisted key", "key_prefix", existing[:min(12, len(existing))]+"...")
				reused = true
			} else {
				// Key is dead (revoked/invalid) — fall through to generate new one.
				b.logger.Info("persisted key invalid, generating new one")
				b.apiKey = ""
			}
		}
	}

	if !reused {
		// Demo credentials are provisioned inside the trusted server process. They
		// never need the retired public account-key registration endpoint.
		apiKey, err := b.credentialProvisioner(ctx, b.config)
		if err != nil {
			return fmt.Errorf("provision demo credential: %w", err)
		}
		b.apiKey = apiKey

		if len(b.apiKey) > 12 {
			b.logger.Info("registered demo bot", "key_prefix", b.apiKey[:12]+"...")
		} else {
			b.logger.Info("registered demo bot")
		}

		// Persist the key for next restart.
		if db.Pool != nil {
			if err := db.SaveDemoBotKey(ctx, b.config.Name, b.apiKey); err != nil {
				b.logger.Warn("failed to persist demo bot key", "error", err)
			}
		}

		// Configure the bot name and avatar.
		if err := b.configure(ctx); err != nil {
			b.logger.Warn("failed to configure bot, continuing with defaults", "error", err)
		}
	}
	if err := b.configureCosmetics(ctx); err != nil {
		b.logger.Warn("failed to configure cosmetics, continuing with standard visuals", "pack", b.config.CosmeticPackID, "error", err)
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

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("config failed: HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var response struct {
		BotID string `json:"bot_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("decode config response: %w", err)
	}
	if response.BotID == "" {
		return errors.New("config response did not include bot_id")
	}
	b.botID = response.BotID

	b.logger.Info("configured bot", "name", b.config.Name, "color", b.config.Color)
	return nil
}

func (b *demoBot) configureCosmetics(ctx context.Context) error {
	if b.config.CosmeticPackID == "" {
		return nil
	}
	selections, err := b.cosmeticProvisioner(ctx, b.botID, b.config)
	if err != nil {
		return err
	}
	if len(selections) != 3 {
		return fmt.Errorf("cosmetic pack %q resolved to %d items", b.config.CosmeticPackID, len(selections))
	}
	b.logger.Info("equipped demo cosmetic pack", "pack", b.config.CosmeticPackID)
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
		} else {
			// Successful session — reset backoff.
			backoff = 1.0
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
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: true,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Set read deadline and keep it fresh — reconnect if no data for 45s.
	conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		return nil
	})

	// Start a ping goroutine to keep the connection alive.
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(
					websocket.PingMessage, nil,
					time.Now().Add(5*time.Second),
				); err != nil {
					return
				}
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(pingDone)

	// 1. Read "connected" message.
	msg, err := readJSON(conn)
	if err != nil {
		return fmt.Errorf("read connected: %w", err)
	}
	if msgType, _ := msg["type"].(string); msgType != "connected" {
		return fmt.Errorf("expected 'connected', got %q", msgType)
	}
	if id, ok := msg["bot_id"].(string); ok {
		b.botID = id
	}

	// 2. Send "select_loadout".
	b.applyConfiguredStrategy("session_start")
	loadout := map[string]interface{}{
		"type":              "select_loadout",
		"weapon":            b.config.Weapon,
		"stats":             b.config.Stats,
		"fallback_behavior": fallbackBehaviorForStrategy(b.strategy),
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
	// Extract computed attack_range and max_hp from server.
	if comp, ok := msg["computed"].(map[string]interface{}); ok {
		if ar, ok := comp["attack_range"].(float64); ok {
			b.attackRange = int(ar)
		}
		if mhp, ok := comp["max_hp"].(float64); ok {
			b.maxHP = mhp
		}
	}

	b.logger.Info("entered arena", "weapon", b.config.Weapon, "strategy", b.strategy,
		"attack_range", b.attackRange, "max_hp", b.maxHP)
	if err := b.fetchMap(ctx); err != nil {
		b.logger.Debug("map prefetch failed", "error", err)
	}

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
		// Refresh read deadline on every message.
		conn.SetReadDeadline(time.Now().Add(45 * time.Second))

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "tick":
			if !shouldActOnTick(msg) {
				continue
			}
			action := PickAction(b.strategy, msg, b.config.Weapon, b.attackRange, b.botID)
			payload := buildActionPayload(msg["tick"], action)
			if err := conn.WriteJSON(payload); err != nil {
				return fmt.Errorf("send action: %w", err)
			}

		case "death":
			resetMineCount(b.botID)
			resetGravWell(b.botID)

		case "respawn":
			// Bot is alive again.

		case "round_end":
			if err := b.refreshStats(ctx); err != nil {
				b.logger.Debug("stats refresh failed", "error", err)
			}
			if err := b.fetchMap(ctx); err != nil {
				b.logger.Debug("map refresh failed", "error", err)
			}

		case "map_init":
			parseTerrain(msg)

		case "round_start":
			resetMineCount(b.botID)
			resetGravWell(b.botID)
			b.applyConfiguredStrategy("round_start")
			if err := b.fetchMap(ctx); err != nil {
				b.logger.Debug("map refresh failed", "error", err)
			}

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

func shouldActOnTick(msg map[string]interface{}) bool {
	state, ok := msg["your_state"].(map[string]interface{})
	if !ok {
		return false
	}
	alive, ok := state["is_alive"].(bool)
	return ok && alive
}

// buildActionPayload converts an AI decision to the public bot protocol. Keep
// every action field here so new tactics cannot silently disappear between
// PickAction and the WebSocket (charged bow shots previously lost Charged).
func buildActionPayload(tick interface{}, action actionResult) map[string]interface{} {
	payload := map[string]interface{}{
		"type":   "action",
		"tick":   tick,
		"action": action.Action,
	}
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
	if action.Charged {
		payload["charged"] = true
	}
	return payload
}

func (b *demoBot) fetchMap(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.serverURL+"/api/v1/arena/map", nil)
	if err != nil {
		return err
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("arena map failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode arena map: %w", err)
	}
	if status, _ := payload["status"].(string); status != "ok" {
		return nil
	}

	parseTerrain(payload)
	return nil
}

func (b *demoBot) refreshStats(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.serverURL+"/api/v1/bot/stats", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Arena-Key", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bot stats failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stats struct {
		Elo          int `json:"elo"`
		Rank         int `json:"rank"`
		Kills        int `json:"kills"`
		Deaths       int `json:"deaths"`
		RoundsPlayed int `json:"rounds_played"`
		RoundWins    int `json:"round_wins"`
		BestStreak   int `json:"best_streak"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return fmt.Errorf("decode bot stats: %w", err)
	}

	b.logger.Info("lifetime stats",
		"elo", stats.Elo,
		"rank", stats.Rank,
		"kills", stats.Kills,
		"deaths", stats.Deaths,
		"rounds_played", stats.RoundsPlayed,
		"round_wins", stats.RoundWins,
		"best_streak", stats.BestStreak,
	)
	return nil
}

func fallbackBehaviorForStrategy(strategy string) string {
	switch strategy {
	case "defensive":
		return "defensive"
	case "territorial":
		return "territorial"
	case "assassin":
		return "hunter"
	case "kite":
		return "opportunistic"
	case "berserker":
		return "aggressive"
	case "hunter", "opportunistic", "aggressive":
		return strategy
	default:
		return "aggressive"
	}
}

// configuredStrategy keeps each demo template on its declared archetype. The
// automatic balancer needs stable, reproducible cohorts; rerolling every bot
// from the same weapon-wide pool made paired templates behaviorally identical
// and added strategy noise to weapon performance samples.
func configuredStrategy(weapon, preferred string) string {
	choices := strategyPoolForWeapon(weapon)
	if len(choices) == 0 {
		if preferred != "" {
			return preferred
		}
		return "aggressive"
	}
	for _, choice := range choices {
		if preferred == choice {
			return preferred
		}
	}
	return choices[0]
}

func strategyPoolForWeapon(weapon string) []string {
	switch weapon {
	case "shield":
		return []string{"territorial", "aggressive", "defensive"}
	case "staff":
		return []string{"kite", "defensive", "aggressive"}
	case "bow":
		return []string{"kite", "assassin", "defensive"}
	case "daggers":
		return []string{"assassin", "berserker", "aggressive"}
	case "grapple":
		return []string{"assassin", "aggressive", "territorial"}
	case "spear":
		return []string{"aggressive", "territorial", "berserker"}
	case "sword":
		return []string{"aggressive", "berserker", "territorial", "defensive"}
	default:
		return []string{"aggressive", "defensive", "territorial", "assassin", "kite", "berserker"}
	}
}

func (b *demoBot) applyConfiguredStrategy(reason string) {
	prev := b.strategy
	next := configuredStrategy(b.config.Weapon, b.config.Strategy)
	b.strategy = next
	if prev != next {
		b.logger.Info("strategy restored", "reason", reason, "from", prev, "to", next)
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
