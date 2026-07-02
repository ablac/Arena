package game

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
	viewCache map[int][]map[string]interface{}
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
	kf.viewCache = nil
}

// RecentViews returns the last count entries serialised for tick messages.
// The result is cached until the feed changes; callers must treat it as
// read-only (it is shared across every bot's message in a tick).
func (kf *KillFeed) RecentViews(count int) []map[string]interface{} {
	if views, ok := kf.viewCache[count]; ok {
		return views
	}
	recent := kf.GetRecent(count)
	views := make([]map[string]interface{}, 0, len(recent))
	for _, kfe := range recent {
		views = append(views, map[string]interface{}{
			"killer": kfe.Killer,
			"victim": kfe.Victim,
			"weapon": kfe.Weapon,
			"tick":   kfe.Tick,
		})
	}
	if kf.viewCache == nil {
		kf.viewCache = make(map[int][]map[string]interface{}, 2)
	}
	kf.viewCache[count] = views
	return views
}

// AllViews returns every entry serialised, cached until the feed changes.
func (kf *KillFeed) AllViews() []map[string]interface{} {
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
	kf.viewCache = nil
}
