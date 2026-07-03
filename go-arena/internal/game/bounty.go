package game

import (
	"arena-server/internal/db"
	"arena-server/internal/config"
	"math"
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
	RewardMultiplier float64
	entries    map[string]*BountyEntry
}

// NewBountySystem creates an empty bounty system.
func NewBountySystem() *BountySystem {
	return &BountySystem{
		RewardMultiplier: 1,
		entries:          make(map[string]*BountyEntry),
	}
}

// Clear wipes the entire bounty state. Used only for full resets/restarts.
func (bs *BountySystem) Clear() {
	bs.TargetID = ""
	bs.TargetName = ""
	bs.RewardMultiplier = 1
	bs.entries = make(map[string]*BountyEntry)
}

// ResetRoundState clears only transient per-round target markers while keeping
// the persistent bounty board intact across rounds.
func (bs *BountySystem) ResetRoundState(bots map[string]*BotState) {
	bs.TargetID = ""
	bs.TargetName = ""
	if bs.RewardMultiplier <= 0 {
		bs.RewardMultiplier = 1
	}
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

	// Issue #14: the crown must not flap between tied bots. Two rules make the
	// pick stable across ticks: (1) incumbent hysteresis - if the current
	// target is still alive and on the board, it starts as the best, so a
	// challenger must be STRICTLY better on (BountyPoints, WinStreak) to
	// dethrone it; a tie keeps the incumbent. (2) deterministic tiebreak by
	// BotID - Go randomizes map iteration order every range, so without this a
	// first-time tie between two equal bots would still pick a random winner
	// per tick. Lowest BotID wins a genuine tie, so the initial pick is stable.
	var bestBot *BotState
	var bestEntry *BountyEntry
	bestIsIncumbent := false
	if bs.TargetID != "" {
		if incumbent, ok := bots[bs.TargetID]; ok && incumbent.IsAlive {
			if entry, ok := bs.entries[bs.TargetID]; ok {
				bestBot = incumbent
				bestEntry = entry
				bestIsIncumbent = true
			}
		}
	}
	for botID, entry := range bs.entries {
		bot, ok := bots[botID]
		if !ok || !bot.IsAlive {
			continue
		}
		if bestEntry == nil {
			bestEntry = entry
			bestBot = bot
			continue
		}
		if bot.BotID == bestBot.BotID {
			continue // the incumbent, already seeded as best
		}
		strictlyBetter := entry.BountyPoints > bestEntry.BountyPoints ||
			(entry.BountyPoints == bestEntry.BountyPoints && entry.WinStreak > bestEntry.WinStreak)
		// Incumbent keeps the crown on a genuine tie (hysteresis). Only a
		// no-incumbent leader is decided among equals by lowest BotID, so the
		// first-ever pick is deterministic instead of map-order random.
		tiedButLowerID := !bestIsIncumbent &&
			entry.BountyPoints == bestEntry.BountyPoints &&
			entry.WinStreak == bestEntry.WinStreak &&
			bot.BotID < bestBot.BotID
		if strictlyBetter || tiedButLowerID {
			bestEntry = entry
			bestBot = bot
			bestIsIncumbent = false
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

	mult := bs.RewardMultiplier
	if mult <= 0 {
		mult = 1
	}
	bonus := int(math.Round(float64(entry.BountyPoints) * mult))
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
