package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

// DashboardHandler holds references for dashboard endpoints.
type DashboardHandler struct {
	Engine    interface{ DashboardEngine }
	Bus       *EventBus
	Admin     *AdminHandler
	startTime time.Time
}

// DashboardEngine is the interface the dashboard needs from the game engine.
type DashboardEngine interface {
	ConnectedBotCount() int
	SpectatorCount() int
	IsPaused() bool
	GetFullGameState() map[string]interface{}
	GetBotDetail(botID string) (map[string]interface{}, bool)
	ListAllBots() []map[string]interface{}
	ListConnections() map[string]interface{}
}

// NewDashboardHandler creates a dashboard handler.
func NewDashboardHandler(bus *EventBus, admin *AdminHandler) *DashboardHandler {
	return &DashboardHandler{
		Bus:       bus,
		Admin:     admin,
		startTime: admin.startTime,
	}
}

// DashboardRoutes registers all dashboard routes.
func (dh *DashboardHandler) DashboardRoutes(r chi.Router) {
	r.Get("/overview", dh.overview)
	r.Get("/connections", dh.connections)
	r.Get("/bots", dh.bots)
	r.Get("/bots/{id}", dh.botDetail)
	r.Get("/http-log", dh.httpLog)
	r.Get("/ws-messages", dh.wsMessages)
	r.Get("/errors", dh.errors)
	r.Get("/rate-limits", dh.rateLimits)
	r.Get("/game-events", dh.gameEvents)

	// SSE streaming endpoints.
	r.Get("/stream/connections", dh.streamConnections)
	r.Get("/stream/http-log", dh.streamHTTPLog)
	r.Get("/stream/ws-messages", dh.streamWSMessages)
	r.Get("/stream/game-events", dh.streamGameEvents)
	r.Get("/stream/all", dh.streamAll)
}

// overview returns server overview data.
func (dh *DashboardHandler) overview(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(dh.startTime)
	engine := dh.Admin.Engine

	data := map[string]interface{}{
		"bots_online": engine.ConnectedBotCount(),
		"spectators":  engine.SpectatorCount(),
		"tick_rate":   config.C.TickRate,
		"uptime":      uptime.Round(time.Second).String(),
		"uptime_secs": int(uptime.Seconds()),
		"paused":      engine.IsPaused(),
		"goroutines":  runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc_mb":      fmt.Sprintf("%.1f", float64(memStats.Alloc)/1024/1024),
			"sys_mb":        fmt.Sprintf("%.1f", float64(memStats.Sys)/1024/1024),
			"heap_alloc_mb": fmt.Sprintf("%.1f", float64(memStats.HeapAlloc)/1024/1024),
			"heap_objects":  memStats.HeapObjects,
			"gc_runs":       memStats.NumGC,
		},
		"game_state": engine.GetFullGameState(),
		"event_counts": map[string]int{
			"connections": dh.Bus.Connections.Count(),
			"http_log":    dh.Bus.HTTPLog.Count(),
			"game_events": dh.Bus.GameEvents.Count(),
			"ws_messages": dh.Bus.WSMessages.Count(),
			"rate_limits": dh.Bus.RateLimits.Count(),
		},
	}
	writeJSON(w, http.StatusOK, data)
}

// connections returns recent connection events.
func (dh *DashboardHandler) connections(w http.ResponseWriter, r *http.Request) {
	events := dh.Bus.Connections.GetAll()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// bots returns all connected bots.
func (dh *DashboardHandler) bots(w http.ResponseWriter, r *http.Request) {
	bots := dh.Admin.Engine.ListAllBots()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bots":  bots,
		"count": len(bots),
	})
}

// botDetail returns detail for a single bot.
func (dh *DashboardHandler) botDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	detail, found := dh.Admin.Engine.GetBotDetail(id)
	if !found {
		writeStructuredError(w, dh.Bus, http.StatusNotFound, "bot not found", "BOT_NOT_FOUND", map[string]interface{}{"bot_id": id})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// httpLog returns recent HTTP request logs.
func (dh *DashboardHandler) httpLog(w http.ResponseWriter, r *http.Request) {
	events := dh.Bus.HTTPLog.GetAll()

	// Apply filters.
	statusFilter := r.URL.Query().Get("status")
	pathFilter := r.URL.Query().Get("path")
	ipFilter := r.URL.Query().Get("ip")

	if statusFilter != "" || pathFilter != "" || ipFilter != "" {
		filtered := make([]DashboardEvent, 0)
		for _, evt := range events {
			if statusFilter != "" {
				s, _ := evt.Data["status"].(int)
				if fmt.Sprintf("%d", s) != statusFilter && !strings.HasPrefix(fmt.Sprintf("%d", s), statusFilter[:1]) {
					continue
				}
			}
			if pathFilter != "" {
				p, _ := evt.Data["path"].(string)
				if !strings.Contains(p, pathFilter) {
					continue
				}
			}
			if ipFilter != "" {
				ip, _ := evt.Data["ip"].(string)
				if !strings.Contains(ip, ipFilter) {
					continue
				}
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// wsMessages returns recent WS messages.
func (dh *DashboardHandler) wsMessages(w http.ResponseWriter, r *http.Request) {
	events := dh.Bus.WSMessages.GetAll()

	// Filter by bot_id if specified.
	botFilter := r.URL.Query().Get("bot_id")
	if botFilter != "" {
		filtered := make([]DashboardEvent, 0)
		for _, evt := range events {
			bid, _ := evt.Data["bot_id"].(string)
			if bid == botFilter {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// errors returns aggregated errors.
func (dh *DashboardHandler) errors(w http.ResponseWriter, r *http.Request) {
	errs := dh.Bus.Errors.GetAll()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"errors": errs,
		"count":  len(errs),
	})
}

// rateLimits returns current rate limit state from Redis.
func (dh *DashboardHandler) rateLimits(w http.ResponseWriter, r *http.Request) {
	events := dh.Bus.RateLimits.GetAll()

	// Also try to get live rate limit keys from Redis.
	var redisState []map[string]interface{}
	if security.RedisClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		keys, err := security.RedisClient.Keys(ctx, "ratelimit:*").Result()
		if err == nil {
			for _, key := range keys {
				count, _ := security.RedisClient.Get(ctx, key).Int()
				ttl, _ := security.RedisClient.TTL(ctx, key).Result()
				redisState = append(redisState, map[string]interface{}{
					"key":   strings.TrimPrefix(key, "ratelimit:"),
					"count": count,
					"ttl":   int(ttl.Seconds()),
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recent_events": events,
		"redis_state":   redisState,
		"event_count":   len(events),
		"redis_count":   len(redisState),
	})
}

// gameEvents returns recent game events.
func (dh *DashboardHandler) gameEvents(w http.ResponseWriter, r *http.Request) {
	events := dh.Bus.GameEvents.GetAll()

	// Filter by event type.
	eventFilter := r.URL.Query().Get("event")
	if eventFilter != "" {
		filtered := make([]DashboardEvent, 0)
		for _, evt := range events {
			e, _ := evt.Data["event"].(string)
			if e == eventFilter {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// --------------------------------------------------------------------------
// SSE streaming
// --------------------------------------------------------------------------

func (dh *DashboardHandler) streamConnections(w http.ResponseWriter, r *http.Request) {
	dh.handleSSE(w, r, EventConnection)
}

func (dh *DashboardHandler) streamHTTPLog(w http.ResponseWriter, r *http.Request) {
	dh.handleSSE(w, r, EventHTTPRequest)
}

func (dh *DashboardHandler) streamWSMessages(w http.ResponseWriter, r *http.Request) {
	dh.handleSSE(w, r, EventWSMessage)
}

func (dh *DashboardHandler) streamGameEvents(w http.ResponseWriter, r *http.Request) {
	dh.handleSSE(w, r, EventGameEvent)
}

func (dh *DashboardHandler) streamAll(w http.ResponseWriter, r *http.Request) {
	dh.handleSSE(w, r, "")
}

func (dh *DashboardHandler) handleSSE(w http.ResponseWriter, r *http.Request, filter EventType) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := dh.Bus.Subscribe(filter)
	defer dh.Bus.Unsubscribe(sub)

	// Send initial keepalive.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	lastSentID := int64(0)
	writeEvent := func(evt DashboardEvent) bool {
		// The subscription is installed before history is read so no event can
		// be missed. An event can therefore appear in both replay and the
		// subscriber queue; the shared global ID makes that duplicate safe to
		// suppress here.
		if evt.ID <= lastSentID {
			return false
		}
		data, err := json.Marshal(evt)
		if err != nil {
			slog.Error("failed to marshal SSE event", "error", err)
			return false
		}
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data)
		lastSentID = evt.ID
		return true
	}

	// Optionally send recent history.
	sinceStr := r.URL.Query().Get("since_id")
	if sinceStr != "" {
		sinceID, err := strconv.ParseInt(sinceStr, 10, 64)
		if err == nil && sinceID > 0 {
			lastSentID = sinceID
		}
		var buf *RingBuffer
		switch filter {
		case EventConnection, EventAuthFailure:
			buf = dh.Bus.Connections
		case EventHTTPRequest:
			buf = dh.Bus.HTTPLog
		case EventGameEvent:
			buf = dh.Bus.GameEvents
		case EventWSMessage:
			buf = dh.Bus.WSMessages
		default:
			buf = dh.Bus.AllEvents
		}
		for _, evt := range buf.GetSince(sinceID) {
			writeEvent(evt)
		}
		flusher.Flush()
	}

	ctx := r.Context()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.Events:
			if writeEvent(evt) {
				flusher.Flush()
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// --------------------------------------------------------------------------
// DB-based rate limit info
// --------------------------------------------------------------------------

func getRateLimitInfo(ctx context.Context) []map[string]interface{} {
	if db.Pool == nil {
		return nil
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT ip_address, keys_generated, window_start FROM rate_limits WHERE window_start > NOW() - INTERVAL '1 hour' ORDER BY keys_generated DESC LIMIT 50`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var rl db.RateLimit
		if err := rows.Scan(&rl.IPAddress, &rl.KeysGenerated, &rl.WindowStart); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"ip":             rl.IPAddress,
			"keys_generated": rl.KeysGenerated,
			"window_start":   rl.WindowStart,
		})
	}
	return results
}
