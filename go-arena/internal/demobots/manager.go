package demobots

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// Manager manages the lifecycle of all demo bots. It spawns each bot as a
// goroutine and handles graceful shutdown.
type Manager struct {
	serverURL string
	count     int
	bots      []*demoBot
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewManager creates a Manager that will spawn count demo bots connecting to
// serverURL. If count <= 0, all 15 configs are used. If count > 15, configs
// are cycled (e.g. bot 16 uses config 0, bot 17 uses config 1, etc.).
func NewManager(serverURL string, count int) *Manager {
	if count <= 0 {
		count = len(DemoConfigs)
	}

	bots := make([]*demoBot, count)
	for i := 0; i < count; i++ {
		cfg := DemoConfigs[i%len(DemoConfigs)]
		// If cycling past the first 15, append a suffix to avoid name collisions.
		if i >= len(DemoConfigs) {
			cfg.Name = cfg.Name + fmt.Sprintf("-%d", i/len(DemoConfigs)+1)
		}
		bots[i] = newDemoBot(cfg, serverURL)
	}

	return &Manager{
		serverURL: serverURL,
		count:     count,
		bots:      bots,
	}
}

// Start launches all demo bots as goroutines, staggered by 1-2 seconds each.
// It blocks until all bots are launched (not until they finish). Each bot
// reconnects automatically on disconnection. Call Stop to shut them down.
func (m *Manager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)

	slog.Info("starting demo bots", "count", m.count)

	// Give the HTTP server a moment to start accepting connections.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}

	for i, bot := range m.bots {
		// Register each bot via REST before launching its WS goroutine.
		if err := bot.register(ctx); err != nil {
			slog.Error("failed to register demo bot, skipping",
				"bot", bot.config.Name, "error", err)
			continue
		}

		m.wg.Add(1)
		go func(b *demoBot) {
			defer m.wg.Done()
			b.run(ctx)
		}(bot)

		// Stagger launches to avoid overwhelming the server.
		if i < len(m.bots)-1 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(1000+rand.Intn(1000)) * time.Millisecond):
			}
		}
	}

	slog.Info("all demo bots launched", "count", m.count)
}

// Stop gracefully shuts down all demo bots and waits for them to finish.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	slog.Info("all demo bots stopped")
}
