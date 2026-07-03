package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"arena-server/internal/api"
	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/demobots"
	"arena-server/internal/game"
	"arena-server/internal/security"
	"arena-server/internal/ws"
)

func main() {
	// Load configuration from environment variables.
	config.Load()

	// Connect to the database. By default the DB is required (auth and
	// persistence depend on it): if it is still unreachable after retries we
	// exit non-zero so the container restart policy retries from a clean
	// start, rather than silently running in a broken degraded mode where
	// every keyed bot join fails (the 2026-05-29 incident). Set
	// ARENA_DB_OPTIONAL=true to allow running without a database.
	ctx := context.Background()
	if err := db.Connect(ctx); err != nil {
		if !config.C.DBOptional {
			slog.Error("database required but unavailable after retries; exiting for restart", "error", err)
			os.Exit(1)
		}
		slog.Warn("database not available, running without persistence (ARENA_DB_OPTIONAL=true)", "error", err)
	}
	defer db.Close()

	// Ensure the database schema for persistence-backed features.
	if db.Pool != nil {
		if err := db.EnsureCoreSchema(ctx); err != nil {
			slog.Warn("failed to ensure database schema", "error", err)
		}
	}

	// Initialise Redis for rate limiting (optional).
	if err := security.InitRedis(); err != nil {
		slog.Warn("redis not available, rate limiting disabled", "error", err)
	}

	// Initialise grid-based weapon ranges from config cell size.
	game.InitWeaponRanges(config.C.PathfindingCellSize)
	if err := game.LoadWeaponBalance(ctx); err != nil {
		slog.Warn("failed to load weapon balance state", "error", err)
	}

	// Create and start the game engine.
	engine := game.NewGameEngine()
	if db.Pool != nil {
		if entries, err := db.ListBountyBoardEntries(ctx); err != nil {
			slog.Warn("failed to load bounty board state", "error", err)
		} else if len(entries) > 0 {
			engine.RestoreBountyBoard(entries)
			slog.Info("restored bounty board state", "entries", len(entries))
		} else if seed, err := db.GetLatestWinnerBountySeed(
			ctx,
			config.C.BountyWinStreakThreshold,
			config.C.BountyBoardBasePoints,
			config.C.BountyBoardStepPoints,
			config.C.BountyBoardMaxPoints,
		); err != nil {
			slog.Warn("failed to seed bounty board from recent rounds", "error", err)
		} else if seed != nil {
			engine.RestoreBountyBoard([]db.BountyBoardEntry{*seed})
			slog.Info("seeded bounty board from recent winners", "bot", seed.Name, "streak", seed.WinStreak)
		}
	}
	go engine.Run(ctx)

	// Demo bots: create manager before router so admin endpoints can reference it.
	var demoManager *demobots.Manager
	if demoBotEnabled() {
		count := demoBotCount()
		localURL := fmt.Sprintf("http://localhost:%d", config.C.ServerPort)
		demoManager = demobots.NewManager(localURL, count)
		slog.Info("demo bots enabled", "count", count)
	}

	// Build the HTTP router with optional demo manager.
	var routerOpts []api.RouterOption
	if demoManager != nil {
		routerOpts = append(routerOpts, api.WithDemoManager(demoManager))
	}
	router := api.NewRouter(engine, routerOpts...)

	// Wire up event hooks for dashboard logging.
	ws.EventHook = func(action, botName, botID, ip, apiKeyID, errMsg string) {
		api.EmitConnection(api.GlobalEventBus, action, botName, botID, ip, apiKeyID, errMsg)
	}
	ws.WSMessageHook = func(botID, botName, action string, data map[string]interface{}) {
		api.EmitWSMessage(api.GlobalEventBus, botID, botName, action, data)
	}
	game.GameEventHook = func(eventName string, data map[string]interface{}) {
		api.EmitGameEvent(api.GlobalEventBus, eventName, data)
	}

	// Start the HTTP server.
	addr := fmt.Sprintf("%s:%d", config.C.ServerHost, config.C.ServerPort)
	slog.Info("starting arena server", "addr", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
		// ReadHeaderTimeout bounds slow-header (Slowloris-style) connections.
		// It only covers the time to read request headers before the handler
		// runs, so it does not affect long-lived WebSocket connections, which
		// hijack the raw net.Conn and manage their own read/write deadlines.
		ReadHeaderTimeout: 10 * time.Second,
		// IdleTimeout only applies to keep-alive HTTP connections sitting
		// between requests, not to hijacked WebSocket connections.
		IdleTimeout: 120 * time.Second,
	}

	// Start demo bots after server setup.
	if demoManager != nil {
		go demoManager.Start(ctx)
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		slog.Info("shutting down...")

		// Stop demo bots first so they disconnect cleanly.
		if demoManager != nil {
			demoManager.Stop()
		}

		engine.Running = false
		srv.Shutdown(context.Background())
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// demoBotEnabled returns true if the ARENA_DEMO_BOTS env var enables demo
// bots. Defaults to "true".
func demoBotEnabled() bool {
	v := strings.ToLower(os.Getenv("ARENA_DEMO_BOTS"))
	if v == "" {
		return true // default: enabled
	}
	return v == "true" || v == "1" || v == "yes"
}

// demoBotCount returns the number of demo bots to spawn from the
// ARENA_DEMO_BOT_COUNT env var. Defaults to the built-in roster size.
func demoBotCount() int {
	v := os.Getenv("ARENA_DEMO_BOT_COUNT")
	if v == "" {
		return len(demobots.DemoConfigs)
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return len(demobots.DemoConfigs)
	}
	return n
}
