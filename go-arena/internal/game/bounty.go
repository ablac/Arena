package game

import (
	"arena-server/internal/db"
	"arena-server/internal/config"
	"sort"
)

// BountyEntry tracks a bot that has built up a cross-round bounty.
type BountyEntry struct {
	BotID         string `json:"bot_id"`
	Name          string `json:"name"`
	AvatarColor   string `json:"avatar_color"`
	Weapon        string `json:"weapon"`
	WinStreak     int    `json:"win_streak"`
	BountyPoints  int    `json:"bounty_points"`
	Claims        int    `json:"claims"`
	IsTarget      bool   `json:"is_target"`
}

// BountySystem tracks the active bounty board plus the current in-round target.
type BountySystem struct {
	TargetID   string
	TargetName string
	entries    map[string]*BountyEntry
}

// NewBountySystem creates an empty bounty system.
func NewBountySystem() *BountySystem {
	return &BountySystem{
		entries: make(map[string]*BountyEntry),
	}
}

// Clear wipes the entire bounty state. Used only for full resets/restarts.
func (bs *BountySystem) Clear() {
	bs.TargetID = ""
	bs.TargetName = ""
	bs.entries = make(map[string]*BountyEntry)
}

// ResetRoundState clears only transient per-round target markers while keeping
// the persistent bounty board intact across rounds.
func (bs *BountySystem) ResetRoundState(bots map[string]*BotState) {
	bs.TargetID = ""
	bs.TargetName = ""
	for _, bot := range bots {
		bot.IsBountyTarget = false
	}
}

// Update recalculates the current in-round bounty target from the persistent
// bounty board. The highest-value alive bounty target gets the crown.
func (bs *BountySystem) Update(bots map[string]*BotState) {
	for _, bot := range bots {
		bot.IsBountyTarget = false
	}

	var bestBot *BotState
	var bestEntry *BountyEntry
	for botID, entry := range bs.entries {
		bot, ok := bots[botID]
		if !ok || !bot.IsAlive {
			continue
		}
		if bestEntry == nil || entry.BountyPoints > bestEntry.BountyPoints ||
			(entry.BountyPoints == bestEntry.BountyPoints && entry.WinStreak > bestEntry.WinStreak) {
			bestEntry = entry
			bestBot = bot
		}
	}

	if bestBot == nil || bestEntry == nil {
		bs.TargetID = ""
		bs.TargetName = ""
		return
	}

	bs.TargetID = bestBot.BotID
	bs.TargetName = bestBot.Name
	bestBot.IsBountyTarget = true
	bestEntry.IsTarget = true
	for botID, entry := range bs.entries {
		if botID != bestBot.BotID {
			entry.IsTarget = false
		}
	}
}

// OnRoundEnd awards or increases bounty value for a bot that is winning
// repeatedly across rounds.
func (bs *BountySystem) OnRoundEnd(bots map[string]*BotState, winnerID string) {
	if winnerID == "" {
		return
	}

	threshold := config.C.BountyWinStreakThreshold
	base := config.C.BountyBoardBasePoints
	step := config.C.BountyBoardStepPoints
	maxPoints := config.C.BountyBoardMaxPoints

	for _, bot := range bots {
		if bot.BotID == winnerID {
			bot.RoundWinStreak++
		} else {
			bot.RoundWinStreak = 0
		}
	}

	winner, ok := bots[winnerID]
	if !ok || winner.RoundWinStreak < threshold {
		return
	}

	entry, ok := bs.entries[winnerID]
	if !ok {
		entry = &BountyEntry{
			BotID:       winner.BotID,
			Name:        winner.Name,
			AvatarColor: winner.AvatarColor,
			Weapon:      winner.Weapon,
		}
		bs.entries[winnerID] = entry
	}

	entry.Name = winner.Name
	entry.AvatarColor = winner.AvatarColor
	entry.Weapon = winner.Weapon
	entry.WinStreak = winner.RoundWinStreak

	points := base + (winner.RoundWinStreak-threshold)*step
	if points > maxPoints {
		points = maxPoints
	}
	if points > entry.BountyPoints {
		entry.BountyPoints = points
	}
}

// OnKill awards bounty bonus when a bot kills an active bounty target.
func (bs *BountySystem) OnKill(killer, victim *BotState) float64 {
	entry, ok := bs.entries[victim.BotID]
	if !ok {
		return 0
	}

	bonus := entry.BountyPoints
	if bonus > 0 && killer != nil {
		killer.Elo += bonus
		killer.RoundDamageDealt += float64(bonus)
	}

	bs.removeEntry(victim)
	return float64(bonus)
}

// OnDeath removes a bounty entry when the target dies without another bot
// claiming it directly.
func (bs *BountySystem) OnDeath(victim *BotState) {
	if _, ok := bs.entries[victim.BotID]; !ok {
		return
	}
	bs.removeEntry(victim)
}

func (bs *BountySystem) removeEntry(victim *BotState) {
	delete(bs.entries, victim.BotID)
	victim.RoundWinStreak = 0
	victim.IsBountyTarget = false
	if bs.TargetID == victim.BotID {
		bs.TargetID = ""
		bs.TargetName = ""
	}
}

// Snapshot returns the current bounty board sorted from highest value to lowest.
func (bs *BountySystem) Snapshot() []BountyEntry {
	entries := make([]BountyEntry, 0, len(bs.entries))
	for _, entry := range bs.entries {
		entries = append(entries, *entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].BountyPoints != entries[j].BountyPoints {
			return entries[i].BountyPoints > entries[j].BountyPoints
		}
		if entries[i].WinStreak != entries[j].WinStreak {
			return entries[i].WinStreak > entries[j].WinStreak
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

// Restore replaces the current bounty board with persisted entries.
func (bs *BountySystem) Restore(entries []db.BountyBoardEntry) {
	bs.entries = make(map[string]*BountyEntry, len(entries))
	bs.TargetID = ""
	bs.TargetName = ""
	for _, entry := range entries {
		copyEntry := &BountyEntry{
			BotID:         entry.BotID,
			Name:          entry.Name,
			AvatarColor:   entry.AvatarColor,
			Weapon:        entry.Weapon,
			WinStreak:     entry.WinStreak,
			BountyPoints:  entry.BountyPoints,
			Claims:        entry.Claims,
			IsTarget:      entry.IsTarget,
		}
		bs.entries[entry.BotID] = copyEntry
		if entry.IsTarget {
			bs.TargetID = entry.BotID
			bs.TargetName = entry.Name
		}
	}
}

// IsBountyTarget returns true if the given bot ID is the current live target.
func (bs *BountySystem) IsBountyTarget(botID string) bool {
	return bs.TargetID != "" && bs.TargetID == botID
}
