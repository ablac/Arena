package ws

import (
	"fmt"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

const (
	defaultWeapon   = "sword"
	defaultFallback = "aggressive"
)

type clientViolationError struct {
	Code    string
	Message string
	Details map[string]interface{}
}

func (e *clientViolationError) Error() string { return e.Message }

func newClientViolation(code, message string, details map[string]interface{}) error {
	return &clientViolationError{Code: code, Message: message, Details: details}
}

// applySelectedLoadout validates the entire selection before mutating the bot.
// This prevents a mixed loadout where valid fields are applied while an
// over-budget stat block silently falls back to privileged stored defaults.
func applySelectedLoadout(bot *game.BotState, loadout *LoadoutMessage) error {
	if bot == nil || loadout == nil {
		return fmt.Errorf("loadout is required")
	}
	if !security.ValidateWeapon(loadout.Weapon) {
		return fmt.Errorf("invalid weapon %q", loadout.Weapon)
	}
	if err := security.ValidateStats(loadout.Stats); err != nil {
		return fmt.Errorf("invalid stats: %w", err)
	}
	if !security.ValidateFallbackBehavior(loadout.Fallback) {
		return fmt.Errorf("invalid fallback behavior %q", loadout.Fallback)
	}

	bot.Weapon = loadout.Weapon
	bot.Stats = cloneStats(loadout.Stats)
	bot.FallbackBehavior = loadout.Fallback
	return nil
}

func cloneStats(stats map[string]int) map[string]int {
	cloned := make(map[string]int, len(stats))
	for key, value := range stats {
		cloned[key] = value
	}
	return cloned
}

// normalizeStoredLoadout repairs values that predate current validation. The
// returned bool lets the caller persist the repair, while runtime state is
// always built from a valid loadout even if that persistence attempt fails.
func normalizeStoredLoadout(bot *db.Bot) bool {
	if bot == nil {
		return false
	}
	changed := false
	if !security.ValidateWeapon(bot.DefaultWeapon) {
		bot.DefaultWeapon = defaultWeapon
		changed = true
	}
	if err := security.ValidateStats(map[string]int(bot.DefaultStats)); err != nil {
		bot.DefaultStats = db.JSONBStats(balancedDefaultStats())
		changed = true
	} else {
		bot.DefaultStats = db.JSONBStats(cloneStats(map[string]int(bot.DefaultStats)))
	}
	if !security.ValidateFallbackBehavior(bot.DefaultFallback) {
		bot.DefaultFallback = defaultFallback
		changed = true
	}
	return changed
}

func balancedDefaultStats() map[string]int {
	keys := []string{"hp", "speed", "attack", "defense"}
	stats := make(map[string]int, len(keys))
	for _, key := range keys {
		stats[key] = config.C.StatMin
	}
	remaining := config.C.StatBudget - config.C.StatMin*len(keys)
	for remaining > 0 {
		progress := false
		for _, key := range keys {
			if remaining == 0 {
				break
			}
			if stats[key] >= config.C.StatMax {
				continue
			}
			stats[key]++
			remaining--
			progress = true
		}
		if !progress {
			break
		}
	}
	return stats
}

type violationResult struct {
	Strikes    int
	Locked     bool
	RetryAfter time.Duration
}

type violationEntry struct {
	strikes     []time.Time
	lockedUntil time.Time
}

type violationTracker struct {
	mu           sync.Mutex
	entries      map[string]*violationEntry
	strikeLimit  int
	strikeWindow time.Duration
	lockDuration time.Duration
	now          func() time.Time
}

func newViolationTracker(strikeLimit int, strikeWindow, lockDuration time.Duration) *violationTracker {
	if strikeLimit < 1 {
		strikeLimit = 1
	}
	return &violationTracker{
		entries:      make(map[string]*violationEntry),
		strikeLimit:  strikeLimit,
		strikeWindow: strikeWindow,
		lockDuration: lockDuration,
		now:          time.Now,
	}
}

func (t *violationTracker) IsLocked(apiKeyID string) (time.Duration, bool) {
	if apiKeyID == "" {
		return 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	entry := t.entries[apiKeyID]
	if entry == nil || entry.lockedUntil.IsZero() {
		return 0, false
	}
	if !now.Before(entry.lockedUntil) {
		delete(t.entries, apiKeyID)
		return 0, false
	}
	return entry.lockedUntil.Sub(now), true
}

func (t *violationTracker) Record(apiKeyID string) violationResult {
	if apiKeyID == "" {
		return violationResult{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	entry := t.entries[apiKeyID]
	if entry == nil {
		entry = &violationEntry{}
		t.entries[apiKeyID] = entry
	}
	if now.Before(entry.lockedUntil) {
		return violationResult{Strikes: t.strikeLimit, Locked: true, RetryAfter: entry.lockedUntil.Sub(now)}
	}
	if !entry.lockedUntil.IsZero() {
		entry.strikes = nil
		entry.lockedUntil = time.Time{}
	}
	cutoff := now.Add(-t.strikeWindow)
	kept := entry.strikes[:0]
	for _, strike := range entry.strikes {
		if strike.After(cutoff) {
			kept = append(kept, strike)
		}
	}
	entry.strikes = append(kept, now)
	if len(entry.strikes) >= t.strikeLimit {
		entry.strikes = nil
		entry.lockedUntil = now.Add(t.lockDuration)
		return violationResult{Strikes: t.strikeLimit, Locked: true, RetryAfter: t.lockDuration}
	}
	return violationResult{Strikes: len(entry.strikes)}
}

var botViolationTracker = newViolationTracker(3, time.Minute, 30*time.Second)
