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
}
