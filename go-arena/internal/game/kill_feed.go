package game

import "sync"

// KillFeedEntry records a single kill event.
type KillFeedEntry struct {
	Killer string `json:"killer"`
	Victim string `json:"victim"`
	Weapon string `json:"weapon"`
	Tick   int    `json:"tick"`
}

// KillFeed is a capped ring buffer of kill feed entries.
type KillFeed struct {
	entries []KillFeedEntry
	maxSize int

	// Cached serialised views keyed by requested count. Every bot's tick
	// message embeds the same "last N kills" list and the spectator state
	// embeds the full list, so each is built once per change instead of
	// once per consumer per tick.
	//
	// viewMu guards viewCache only. entries are still protected by the
	// engine lock (mutated exclusively under e.mu.Lock). The cache needs its
	// own mutex because RecentViews lazily WRITES the map and is reachable
	// from paths holding only e.mu.RLock (BuildSpectatorState via the
	// exported GetState), where two concurrent readers would otherwise race
	// on the map write — an unrecoverable runtime fatal. Held for the whole
	// lookup+build+store; do not replace with a double-checked read, which
	// still races the store.
	viewMu    sync.Mutex
	viewCache map[int][]KillFeedEntry
}

// NewKillFeed creates a new kill feed that retains at most maxSize entries.
func NewKillFeed(maxSize int) *KillFeed {
	return &KillFeed{
		entries: make([]KillFeedEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add appends a kill entry. If the feed exceeds maxSize, the oldest entry
// is removed.
func (kf *KillFeed) Add(killer, victim, weapon string, tick int) {
	kf.entries = append(kf.entries, KillFeedEntry{
		Killer: killer,
		Victim: victim,
		Weapon: weapon,
		Tick:   tick,
	})
	if len(kf.entries) > kf.maxSize {
		kf.entries = kf.entries[len(kf.entries)-kf.maxSize:]
	}
	kf.viewMu.Lock()
	kf.viewCache = nil
	kf.viewMu.Unlock()
}

// RecentViews returns the last count entries for tick messages. KillFeedEntry
// marshals with the exact protocol keys (killer/victim/weapon/tick), so the
// entries are shared directly. The result is cached until the feed changes;
// callers must treat it as read-only (it is shared across every bot's message
// in a tick and may be marshaled after the engine lock is released).
func (kf *KillFeed) RecentViews(count int) []KillFeedEntry {
	kf.viewMu.Lock()
	defer kf.viewMu.Unlock()
	if views, ok := kf.viewCache[count]; ok {
		return views
	}
	views := kf.GetRecent(count)
	if kf.viewCache == nil {
		kf.viewCache = make(map[int][]KillFeedEntry, 2)
	}
	kf.viewCache[count] = views
	return views
}

// AllViews returns every entry, cached until the feed changes.
func (kf *KillFeed) AllViews() []KillFeedEntry {
	return kf.RecentViews(len(kf.entries))
}

// GetRecent returns the last count entries. If fewer entries exist, all are
// returned.
func (kf *KillFeed) GetRecent(count int) []KillFeedEntry {
	if count >= len(kf.entries) {
		out := make([]KillFeedEntry, len(kf.entries))
		copy(out, kf.entries)
		return out
	}
	start := len(kf.entries) - count
	out := make([]KillFeedEntry, count)
	copy(out, kf.entries[start:])
	return out
}

// GetAll returns a copy of all entries.
func (kf *KillFeed) GetAll() []KillFeedEntry {
	out := make([]KillFeedEntry, len(kf.entries))
	copy(out, kf.entries)
	return out
}

// GetSince returns all entries with Tick strictly greater than the given tick.
func (kf *KillFeed) GetSince(tick int) []KillFeedEntry {
	var out []KillFeedEntry
	for _, e := range kf.entries {
		if e.Tick > tick {
			out = append(out, e)
		}
	}
	return out
}

// Clear removes all entries from the kill feed.
func (kf *KillFeed) Clear() {
	kf.entries = kf.entries[:0]
	kf.viewMu.Lock()
	kf.viewCache = nil
	kf.viewMu.Unlock()
}
