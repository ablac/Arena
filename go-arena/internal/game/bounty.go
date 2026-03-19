package game

import (
	"arena-server/internal/config"
)

// BountySystem tracks the current bounty target.
type BountySystem struct {
	TargetID   string // bot ID of the current bounty target
	TargetName string
}

// NewBountySystem creates an empty bounty system.
func NewBountySystem() *BountySystem {
	return &BountySystem{}
}

// Clear resets the bounty.
func (bs *BountySystem) Clear() {
	bs.TargetID = ""
	bs.TargetName = ""
}

// Update recalculates the bounty target based on kill streaks.
// The bot with the highest kill streak >= threshold gets the bounty.
// If the current bounty target dies, the bounty passes to the next eligible bot.
func (bs *BountySystem) Update(bots map[string]*BotState) {
	threshold := config.C.BountyKillStreakThreshold

	// Find the alive bot with the highest kill streak >= threshold
	var bestBot *BotState
	bestStreak := 0

	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}
		if bot.KillStreak >= threshold && bot.KillStreak > bestStreak {
			bestStreak = bot.KillStreak
			bestBot = bot
		}
	}

	// Clear old bounty flags
	for _, bot := range bots {
		bot.IsBountyTarget = false
	}

	if bestBot != nil {
		bs.TargetID = bestBot.BotID
		bs.TargetName = bestBot.Name
		bestBot.IsBountyTarget = true
	} else {
		bs.TargetID = ""
		bs.TargetName = ""
	}
}

// OnKill checks if the killed bot was the bounty target and awards bonus.
func (bs *BountySystem) OnKill(killer, victim *BotState) float64 {
	if victim.BotID != bs.TargetID || bs.TargetID == "" {
		return 0
	}

	bonus := config.C.BountyBonusPoints
	// Award bonus as extra damage dealt (for scoring)
	killer.RoundDamageDealt += bonus
	return bonus
}

// IsBountyTarget returns true if the given bot ID is the current bounty.
func (bs *BountySystem) IsBountyTarget(botID string) bool {
	return bs.TargetID != "" && bs.TargetID == botID
}
