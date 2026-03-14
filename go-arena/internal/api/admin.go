package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/demobots"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

// AdminHandler holds references needed by admin endpoints.
type AdminHandler struct {
	Engine      *game.GameEngine
	DemoManager *demobots.Manager
	startTime   time.Time
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(engine *game.GameEngine, demoManager *demobots.Manager) *AdminHandler {
	return &AdminHandler{
		Engine:      engine,
		DemoManager: demoManager,
		startTime:   time.Now(),
	}
}

// AdminAuthMiddleware checks the X-Admin-Token header against the configured
// admin token. Localhost requests can bypass if ARENA_ADMIN_LOCALHOST_BYPASS is true.
func AdminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := &config.C

		// Check localhost bypass.
		if cfg.AdminLocalhostBypass && isLocalhost(r) {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing X-Admin-Token header")
			return
		}

		if cfg.AdminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "admin token not configured")
			return
		}

		if token != cfg.AdminToken {
			writeError(w, http.StatusForbidden, "invalid admin token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLocalhost returns true if the request originates from a loopback address.
func isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// Routes registers all admin routes on the given chi.Router.
func (h *AdminHandler) Routes(r chi.Router) {
	// Demo bot management.
	r.Get("/demobots", h.listDemoBots)
	r.Post("/demobots/start", h.startDemoBots)
	r.Post("/demobots/stop", h.stopDemoBots)
	r.Delete("/demobots/{name}", h.stopDemoBotByName)

	// Debug / inspection.
	r.Get("/debug/connections", h.debugConnections)
	r.Get("/debug/game-state", h.debugGameState)
	r.Get("/debug/bot/{id}", h.debugBot)
	r.Get("/debug/metrics", h.debugMetrics)
	r.Get("/debug/rounds", h.debugRounds)

	// Bot admin.
	r.Post("/bots/{id}/kick", h.kickBot)
	r.Post("/bots/{id}/ban", h.banBot)
	r.Post("/bots/{id}/kill", h.killBot)
	r.Post("/bots/{id}/teleport", h.teleportBot)
	r.Post("/bots/{id}/heal", h.healBot)
	r.Get("/bots", h.listBots)

	// Game control.
	r.Post("/game/pause", h.gamePause)
	r.Post("/game/resume", h.gameResume)
	r.Post("/game/restart-round", h.gameRestartRound)
	r.Put("/game/config", h.updateGameConfig)
	r.Get("/game/config", h.getGameConfig)

	// Data management.
	r.Get("/db/stats", h.dbStats)
	r.Post("/db/reset-leaderboard", h.resetLeaderboard)
	r.Post("/db/cleanup-stale", h.cleanupStale)
	r.Get("/logs", h.getLogs)

	// Server.
	r.Get("/config", h.getServerConfig)
	r.Get("/health/deep", h.deepHealthCheck)
	r.Post("/server/gc", h.triggerGC)
}

// ============================================================================
// Demo bot management
// ============================================================================

func (h *AdminHandler) listDemoBots(w http.ResponseWriter, r *http.Request) {
	if h.DemoManager == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"demo_bots": []interface{}{},
			"count":     0,
			"message":   "demo bot manager not initialized",
		})
		return
	}

	bots := h.DemoManager.ListBots()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"demo_bots": bots,
		"count":     len(bots),
	})
}

func (h *AdminHandler) startDemoBots(w http.ResponseWriter, r *http.Request) {
	if h.DemoManager == nil {
		writeError(w, http.StatusServiceUnavailable, "demo bot manager not initialized")
		return
	}

	var req struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Count <= 0 || req.Count > 50 {
		writeError(w, http.StatusBadRequest, "count must be between 1 and 50")
		return
	}

	names := h.DemoManager.StartN(req.Count)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"started": names,
		"count":   len(names),
	})
}

func (h *AdminHandler) stopDemoBots(w http.ResponseWriter, r *http.Request) {
	if h.DemoManager == nil {
		writeError(w, http.StatusServiceUnavailable, "demo bot manager not initialized")
		return
	}

	h.DemoManager.Stop()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "all demo bots stopped",
	})
}

func (h *AdminHandler) stopDemoBotByName(w http.ResponseWriter, r *http.Request) {
	if h.DemoManager == nil {
		writeError(w, http.StatusServiceUnavailable, "demo bot manager not initialized")
		return
	}

	name := chi.URLParam(r, "name")
	if found := h.DemoManager.StopByName(name); !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("demo bot %q not found", name))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("demo bot %q stopped", name),
	})
}

// ============================================================================
// Debug / inspection
// ============================================================================

func (h *AdminHandler) debugConnections(w http.ResponseWriter, r *http.Request) {
	conns := h.Engine.ListConnections()
	writeJSON(w, http.StatusOK, conns)
}

func (h *AdminHandler) debugGameState(w http.ResponseWriter, r *http.Request) {
	state := h.Engine.GetFullGameState()
	writeJSON(w, http.StatusOK, state)
}

func (h *AdminHandler) debugBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	detail, found := h.Engine.GetBotDetail(id)
	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found", id))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *AdminHandler) debugMetrics(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(h.startTime)

	metrics := map[string]interface{}{
		"goroutines":     runtime.NumGoroutine(),
		"go_version":     runtime.Version(),
		"num_cpu":        runtime.NumCPU(),
		"uptime_seconds": int(uptime.Seconds()),
		"uptime_human":   uptime.Round(time.Second).String(),
		"memory": map[string]interface{}{
			"alloc_mb":       float64(memStats.Alloc) / 1024 / 1024,
			"total_alloc_mb": float64(memStats.TotalAlloc) / 1024 / 1024,
			"sys_mb":         float64(memStats.Sys) / 1024 / 1024,
			"heap_alloc_mb":  float64(memStats.HeapAlloc) / 1024 / 1024,
			"heap_inuse_mb":  float64(memStats.HeapInuse) / 1024 / 1024,
			"heap_objects":   memStats.HeapObjects,
			"stack_inuse_mb": float64(memStats.StackInuse) / 1024 / 1024,
		},
		"gc": map[string]interface{}{
			"num_gc":          memStats.NumGC,
			"last_gc_seconds": float64(memStats.LastGC) / 1e9,
			"pause_total_ms":  float64(memStats.PauseTotalNs) / 1e6,
		},
		"game": map[string]interface{}{
			"tick_count":      h.Engine.ConnectedBotCount(),
			"bots_connected":  h.Engine.ConnectedBotCount(),
			"spectators":      h.Engine.SpectatorCount(),
			"tick_rate":       config.C.TickRate,
		},
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (h *AdminHandler) debugRounds(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	rows, err := db.Pool.Query(r.Context(),
		`SELECT id, round_number, started_at, ended_at, bots_participated, mvp_bot_id, status
		 FROM rounds ORDER BY round_number DESC LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query rounds: "+err.Error())
		return
	}
	defer rows.Close()

	var rounds []map[string]interface{}
	for rows.Next() {
		var rd db.Round
		if err := rows.Scan(&rd.ID, &rd.RoundNumber, &rd.StartedAt, &rd.EndedAt,
			&rd.BotsParticipated, &rd.MVPBotID, &rd.Status); err != nil {
			continue
		}
		entry := map[string]interface{}{
			"id":                rd.ID,
			"round_number":      rd.RoundNumber,
			"started_at":        rd.StartedAt,
			"ended_at":          rd.EndedAt,
			"bots_participated": rd.BotsParticipated,
			"mvp_bot_id":        rd.MVPBotID,
			"status":            rd.Status,
		}
		rounds = append(rounds, entry)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rounds": rounds,
		"count":  len(rounds),
	})
}

// ============================================================================
// Bot admin
// ============================================================================

func (h *AdminHandler) kickBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "kicked by admin"
	}

	if !h.Engine.KickBot(id, req.Reason) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found", id))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("bot %q kicked", id),
		"reason":  req.Reason,
	})
}

func (h *AdminHandler) banBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Get the bot's API key ID before kicking.
	detail, found := h.Engine.GetBotDetail(id)
	if !found {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found", id))
		return
	}

	apiKeyID, _ := detail["api_key_id"].(string)
	if apiKeyID != "" {
		h.Engine.BanKey(apiKeyID)
		// Also deactivate in DB.
		if db.Pool != nil {
			if err := db.DeactivateAPIKey(r.Context(), apiKeyID); err != nil {
				slog.Error("failed to deactivate banned key in DB", "error", err)
			}
		}
	}

	h.Engine.KickBot(id, "banned by admin")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":    fmt.Sprintf("bot %q banned and kicked", id),
		"api_key_id": apiKeyID,
	})
}

func (h *AdminHandler) killBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.Engine.KillBot(id) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found or not alive", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("bot %q killed", id),
	})
}

func (h *AdminHandler) teleportBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate coordinates.
	if req.X < 0 || req.X > config.C.ArenaWidth || req.Y < 0 || req.Y > config.C.ArenaHeight {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("coordinates out of bounds (0-%.0f, 0-%.0f)", config.C.ArenaWidth, config.C.ArenaHeight))
		return
	}

	if !h.Engine.TeleportBot(id, req.X, req.Y) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("bot %q teleported", id),
		"position": [2]float64{req.X, req.Y},
	})
}

func (h *AdminHandler) healBot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		HP float64 `json:"hp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.HP <= 0 {
		writeError(w, http.StatusBadRequest, "hp must be positive")
		return
	}

	if !h.Engine.HealBot(id, req.HP) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("bot %q not found", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": fmt.Sprintf("bot %q healed by %.0f HP", id, req.HP),
	})
}

func (h *AdminHandler) listBots(w http.ResponseWriter, r *http.Request) {
	bots := h.Engine.ListAllBots()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bots":  bots,
		"count": len(bots),
	})
}

// ============================================================================
// Game control
// ============================================================================

func (h *AdminHandler) gamePause(w http.ResponseWriter, r *http.Request) {
	h.Engine.Pause()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "game paused",
		"paused":  true,
	})
}

func (h *AdminHandler) gameResume(w http.ResponseWriter, r *http.Request) {
	h.Engine.Resume()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "game resumed",
		"paused":  false,
	})
}

func (h *AdminHandler) gameRestartRound(w http.ResponseWriter, r *http.Request) {
	h.Engine.ForceRestartRound()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "round restarted",
	})
}

func (h *AdminHandler) getGameConfig(w http.ResponseWriter, r *http.Request) {
	c := &config.C
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tick_rate":          c.TickRate,
		"max_bots":           c.MaxBots,
		"max_spectators":     c.MaxSpectators,
		"view_radius":        c.ViewRadius,
		"arena_width":        c.ArenaWidth,
		"arena_height":       c.ArenaHeight,
		"round_duration":     c.RoundDuration,
		"intermission_time":  c.IntermissionTime,
		"lobby_countdown":    c.LobbyCountdown,
		"min_bots_to_start":  c.MinBotsToStart,
		"stat_budget":        c.StatBudget,
		"zone_damage":        c.ZoneDamagePerTick,
		"zone_shrink_pct":    c.ZoneShrinkPercent,
		"zone_shrink_interval": c.ZoneShrinkInterval,
		"zone_min_radius":    c.ZoneMinRadius,
		"dodge_speed_mult":   c.DodgeSpeedMult,
		"dodge_cooldown":     c.DodgeCooldownTicks,
		"projectile_speed":   c.ProjectileSpeed,
		"afk_timeout_ticks":  c.AFKTimeoutTicks,
	})
}

func (h *AdminHandler) updateGameConfig(w http.ResponseWriter, r *http.Request) {
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	c := &config.C
	applied := make(map[string]interface{})

	for key, val := range updates {
		switch key {
		case "tick_rate":
			if v, ok := toInt(val); ok && v >= 1 && v <= 60 {
				c.TickRate = v
				applied[key] = v
			}
		case "max_bots":
			if v, ok := toInt(val); ok && v >= 1 {
				c.MaxBots = v
				applied[key] = v
			}
		case "max_spectators":
			if v, ok := toInt(val); ok && v >= 0 {
				c.MaxSpectators = v
				applied[key] = v
			}
		case "round_duration":
			if v, ok := toFloat(val); ok && v >= 10 {
				c.RoundDuration = v
				applied[key] = v
			}
		case "intermission_time":
			if v, ok := toFloat(val); ok && v >= 1 {
				c.IntermissionTime = v
				applied[key] = v
			}
		case "lobby_countdown":
			if v, ok := toFloat(val); ok && v >= 1 {
				c.LobbyCountdown = v
				applied[key] = v
			}
		case "min_bots_to_start":
			if v, ok := toInt(val); ok && v >= 1 {
				c.MinBotsToStart = v
				applied[key] = v
			}
		case "zone_damage":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.ZoneDamagePerTick = v
				applied[key] = v
			}
		case "zone_shrink_pct":
			if v, ok := toFloat(val); ok && v >= 0 && v <= 1 {
				c.ZoneShrinkPercent = v
				applied[key] = v
			}
		case "afk_timeout_ticks":
			if v, ok := toInt(val); ok && v >= 0 {
				c.AFKTimeoutTicks = v
				applied[key] = v
			}
		case "view_radius":
			if v, ok := toFloat(val); ok && v > 0 {
				c.ViewRadius = v
				applied[key] = v
			}
		default:
			// Ignore unknown keys.
		}
	}

	slog.Info("admin updated game config", "applied", applied)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "config updated",
		"applied": applied,
	})
}

// ============================================================================
// Data management
// ============================================================================

func (h *AdminHandler) dbStats(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	poolStats := db.Pool.Stat()
	stats := map[string]interface{}{
		"pool": map[string]interface{}{
			"total_conns":      poolStats.TotalConns(),
			"idle_conns":       poolStats.IdleConns(),
			"acquired_conns":   poolStats.AcquiredConns(),
			"max_conns":        poolStats.MaxConns(),
			"constructing_conns": poolStats.ConstructingConns(),
		},
	}

	// Get table row counts.
	tables := []string{"api_keys", "bots", "bot_stats", "kill_log", "rounds"}
	tableCounts := make(map[string]int)
	for _, table := range tables {
		var count int
		// Using fmt.Sprintf here is safe because table names are hardcoded above.
		err := db.Pool.QueryRow(r.Context(),
			fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			tableCounts[table] = -1
		} else {
			tableCounts[table] = count
		}
	}
	stats["table_counts"] = tableCounts

	writeJSON(w, http.StatusOK, stats)
}

func (h *AdminHandler) resetLeaderboard(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Confirm != "RESET_ALL_STATS" {
		writeError(w, http.StatusBadRequest, "must send {\"confirm\": \"RESET_ALL_STATS\"} to confirm")
		return
	}

	_, err := db.Pool.Exec(r.Context(), `TRUNCATE bot_stats`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset leaderboard: "+err.Error())
		return
	}

	slog.Warn("admin reset leaderboard - all stats wiped")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "leaderboard reset successfully",
	})
}

func (h *AdminHandler) cleanupStale(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	var req struct {
		Days int `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Days <= 0 {
		writeError(w, http.StatusBadRequest, "must provide {\"days\": N} where N > 0")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -req.Days)

	result, err := db.Pool.Exec(r.Context(),
		`DELETE FROM api_keys WHERE last_seen < $1 OR (last_seen IS NULL AND created_at < $1)`, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cleanup: "+err.Error())
		return
	}

	affected := result.RowsAffected()
	slog.Info("admin cleaned up stale bots", "days", req.Days, "removed", affected)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     fmt.Sprintf("removed %d stale entries older than %d days", affected, req.Days),
		"removed":     affected,
		"cutoff_date": cutoff.Format(time.RFC3339),
	})
}

func (h *AdminHandler) getLogs(w http.ResponseWriter, r *http.Request) {
	// Read the last N lines from stdout/stderr log.
	// Since Go uses slog which typically writes to stderr, we read from a log
	// file if available. Otherwise, return a message.
	linesStr := r.URL.Query().Get("lines")
	lines := 100
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 && n <= 1000 {
			lines = n
		}
	}

	// Try to read from common log locations.
	logPaths := []string{"/var/log/arena-server.log", "/tmp/arena-server.log"}
	for _, path := range logPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		logLines := strings.Split(string(data), "\n")
		start := 0
		if len(logLines) > lines {
			start = len(logLines) - lines
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"source": path,
			"lines":  logLines[start:],
			"count":  len(logLines[start:]),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "log file not found; logs are written to stderr (use docker logs or journalctl)",
		"lines":   []string{},
	})
}

// ============================================================================
// Server
// ============================================================================

func (h *AdminHandler) getServerConfig(w http.ResponseWriter, r *http.Request) {
	c := &config.C
	// Return sanitized config (no secrets).
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"server_host":    c.ServerHost,
		"server_port":    c.ServerPort,
		"tick_rate":      c.TickRate,
		"max_bots":       c.MaxBots,
		"max_spectators": c.MaxSpectators,
		"arena_size":     [2]float64{c.ArenaWidth, c.ArenaHeight},
		"db_host":        c.DBHost,
		"db_port":        c.DBPort,
		"db_name":        c.DBName,
		"redis_host":     c.RedisHost,
		"redis_port":     c.RedisPort,
		"cors_origins":   c.CORSOrigins,
		"elo_k_factor":   c.EloKFactor,
		"elo_starting":   c.EloStarting,
		"admin_localhost_bypass": c.AdminLocalhostBypass,
		"admin_token_set":       c.AdminToken != "",
	})
}

func (h *AdminHandler) deepHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	health := map[string]interface{}{
		"status":       "ok",
		"uptime":       time.Since(h.startTime).Round(time.Second).String(),
		"goroutines":   runtime.NumGoroutine(),
		"heap_alloc_mb": float64(memStats.HeapAlloc) / 1024 / 1024,
		"bots_online":  h.Engine.ConnectedBotCount(),
		"spectators":   h.Engine.SpectatorCount(),
		"paused":       h.Engine.IsPaused(),
	}

	// DB ping.
	if db.Pool != nil {
		if err := db.Pool.Ping(ctx); err != nil {
			health["db"] = "error: " + err.Error()
			health["status"] = "degraded"
		} else {
			health["db"] = "ok"
		}
	} else {
		health["db"] = "not connected"
		health["status"] = "degraded"
	}

	// Redis ping.
	if security.RedisClient != nil {
		if err := security.RedisClient.Ping(ctx).Err(); err != nil {
			health["redis"] = "error: " + err.Error()
			health["status"] = "degraded"
		} else {
			health["redis"] = "ok"
		}
	} else {
		health["redis"] = "not connected"
	}

	writeJSON(w, http.StatusOK, health)
}

func (h *AdminHandler) triggerGC(w http.ResponseWriter, r *http.Request) {
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	runtime.GC()

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	slog.Info("admin triggered GC",
		"heap_before_mb", float64(before.HeapAlloc)/1024/1024,
		"heap_after_mb", float64(after.HeapAlloc)/1024/1024,
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":          "garbage collection triggered",
		"heap_before_mb":   float64(before.HeapAlloc) / 1024 / 1024,
		"heap_after_mb":    float64(after.HeapAlloc) / 1024 / 1024,
		"freed_mb":         float64(before.HeapAlloc-after.HeapAlloc) / 1024 / 1024,
	})
}

// ============================================================================
// Helpers
// ============================================================================

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
