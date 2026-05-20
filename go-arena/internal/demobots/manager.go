package demobots

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"arena-server/internal/db"
)

// botEntry tracks a running demo bot and its cancel function.
type botEntry struct {
	bot    *demoBot
	cancel context.CancelFunc
}

// Manager manages the lifecycle of demo bots. It supports dynamic
// add/remove of individual bots at runtime.
type Manager struct {
	serverURL string
	mu        sync.Mutex
	bots      map[string]*botEntry // keyed by bot name
	wg        sync.WaitGroup
	parentCtx context.Context
	cancel    context.CancelFunc
	started   bool
}

// NewManager creates a Manager that will spawn count demo bots connecting to
// serverURL. If count <= 0, all 15 configs are used. If count > 15, configs
// are cycled (e.g. bot 16 uses config 0, bot 17 uses config 1, etc.).
func NewManager(serverURL string, count int) *Manager {
	if count <= 0 {
		count = len(DemoConfigs)
	}

	m := &Manager{
		serverURL: serverURL,
		bots:      make(map[string]*botEntry),
	}

	// Pre-create the bot entries (they'll be started in Start).
	for i := 0; i < count; i++ {
		cfg := DemoConfigs[i%len(DemoConfigs)]
		if i >= len(DemoConfigs) {
			cfg.Name = cfg.Name + fmt.Sprintf("-%d", i/len(DemoConfigs)+1)
		}
		bot := newDemoBot(cfg, serverURL)
		m.bots[cfg.Name] = &botEntry{bot: bot}
	}

	return m
}

// Start launches all demo bots as goroutines, staggered by 1-2 seconds each.
// It blocks until all bots are launched (not until they finish). Each bot
// reconnects automatically on disconnection. Call Stop to shut them down.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	ctx, m.cancel = context.WithCancel(ctx)
	m.parentCtx = ctx
	m.started = true

	// Collect bots to start.
	toStart := make([]*botEntry, 0, len(m.bots))
	for _, entry := range m.bots {
		toStart = append(toStart, entry)
	}
	m.mu.Unlock()

	slog.Info("starting demo bots", "count", len(toStart))

	// Ensure DB table for persisted keys.
	if db.Pool != nil {
		if err := db.EnsureDemoBotKeysTable(ctx); err != nil {
			slog.Warn("failed to ensure demo_bot_keys table", "error", err)
		}
	}

	// Give the HTTP server a moment to start accepting connections.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}

	for i, entry := range toStart {
		if ctx.Err() != nil {
			return
		}
		m.launchBot(ctx, entry)

		// Stagger launches — fast enough that all bots join before lobby countdown expires.
		if i < len(toStart)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(200+rand.Intn(300)) * time.Millisecond):
			}
		}
	}

	slog.Info("all demo bots launched", "count", len(toStart))
}

// launchBot registers and runs a single bot entry. Must be called with m.mu unlocked.
func (m *Manager) launchBot(ctx context.Context, entry *botEntry) {
	if err := entry.bot.register(ctx); err != nil {
		slog.Error("failed to register demo bot, skipping",
			"bot", entry.bot.config.Name, "error", err)
		return
	}

	botCtx, botCancel := context.WithCancel(ctx)
	entry.cancel = botCancel

	m.wg.Add(1)
	go func(b *demoBot) {
		defer m.wg.Done()
		b.run(botCtx)
	}(entry.bot)
}

// Stop gracefully shuts down all demo bots and waits for them to finish.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	m.wg.Wait()

	m.mu.Lock()
	m.bots = make(map[string]*botEntry)
	m.started = false
	m.mu.Unlock()

	slog.Info("all demo bots stopped")
}

// StartN spawns N new demo bots dynamically. Returns the names of bots started.
func (m *Manager) StartN(n int) []string {
	m.mu.Lock()
	ctx := m.parentCtx
	if ctx == nil {
		// If Start() hasn't been called, create a background context.
		ctx = context.Background()
		ctx, m.cancel = context.WithCancel(ctx)
		m.parentCtx = ctx
		m.started = true
	}

	existing := len(m.bots)
	var names []string
	var entries []*botEntry

	for i := 0; i < n; i++ {
		idx := (existing + i) % len(DemoConfigs)
		cfg := DemoConfigs[idx]
		suffix := (existing + i) / len(DemoConfigs)
		if suffix > 0 {
			cfg.Name = cfg.Name + fmt.Sprintf("-%d", suffix+1)
		}
		// Avoid name collisions.
		if _, exists := m.bots[cfg.Name]; exists {
			cfg.Name = cfg.Name + fmt.Sprintf("-r%d", rand.Intn(1000))
		}
		bot := newDemoBot(cfg, m.serverURL)
		entry := &botEntry{bot: bot}
		m.bots[cfg.Name] = entry
		entries = append(entries, entry)
		names = append(names, cfg.Name)
	}
	m.mu.Unlock()

	// Launch each bot outside the lock.
	for _, entry := range entries {
		m.launchBot(ctx, entry)
	}

	slog.Info("dynamically started demo bots", "count", n, "names", names)
	return names
}

// StopByName stops a specific demo bot by name. Returns true if found.
func (m *Manager) StopByName(name string) bool {
	m.mu.Lock()
	entry, exists := m.bots[name]
	if !exists {
		m.mu.Unlock()
		return false
	}
	delete(m.bots, name)
	m.mu.Unlock()

	if entry.cancel != nil {
		entry.cancel()
	}
	slog.Info("stopped demo bot", "name", name)
	return true
}

// ListBots returns info about all active demo bots.
func (m *Manager) ListBots() []DemoBotInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	infos := make([]DemoBotInfo, 0, len(m.bots))
	for name, entry := range m.bots {
		infos = append(infos, DemoBotInfo{
			Name:     name,
			Weapon:   entry.bot.config.Weapon,
			Strategy: entry.bot.strategy,
			Color:    entry.bot.config.Color,
			Running:  entry.cancel != nil,
		})
	}
	return infos
}

// Count returns the number of active demo bots.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.bots)
}

// DemoBotInfo holds public info about a demo bot.
type DemoBotInfo struct {
	Name     string `json:"name"`
	Weapon   string `json:"weapon"`
	Strategy string `json:"strategy"`
	Color    string `json:"color"`
	Running  bool   `json:"running"`
}
