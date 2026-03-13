package game

import (
	"math"

	"arena-server/internal/config"
)

// ShouldEndRound returns true when the current round should end, based on
// time expiry, bot count, or all bots being dead/disconnected.
func ShouldEndRound(bots map[string]*BotState, round *RoundState, tickCount int) bool {
	c := &config.C

	// Duration exceeded.
	if (tickCount - round.StartTick) >= int(c.RoundDuration*float64(c.TickRate)) {
		return true
	}

	// All bots disconnected.
	if len(bots) == 0 {
		return true
	}

	// At most 1 bot alive and round started with >= 2.
	alive := 0
	for _, bot := range bots {
		if bot.IsAlive {
			alive++
		}
	}
	if alive <= 1 && len(bots) >= c.MinBotsToStart {
		return true
	}

	// At most 1 bot connected.
	if len(bots) <= 1 {
		return true
	}

	return false
}

// DetermineWinner returns the ID and name of the round winner.
// Priority: last bot alive, then most kills, then first found.
func DetermineWinner(bots map[string]*BotState) (winnerID, winnerName string) {
	// Check for last alive bot.
	var aliveBot *BotState
	aliveCount := 0
	for _, bot := range bots {
		if bot.IsAlive {
			aliveBot = bot
			aliveCount++
		}
	}
	if aliveCount == 1 && aliveBot != nil {
		return aliveBot.BotID, aliveBot.Name
	}

	// Tie or no one alive: most kills wins.
	bestKills := -1
	for _, bot := range bots {
		if bot.RoundKills > bestKills {
			bestKills = bot.RoundKills
			winnerID = bot.BotID
			winnerName = bot.Name
		}
	}
	return winnerID, winnerName
}

// CalculateAwards determines special awards for the round. Each award maps
// an award name to the bot name that earned it.
func CalculateAwards(bots map[string]*BotState) map[string]string {
	awards := make(map[string]string)

	// MVP: most kills (min 1).
	var mvpName string
	mvpKills := 0
	for _, bot := range bots {
		if bot.RoundKills > mvpKills {
			mvpKills = bot.RoundKills
			mvpName = bot.Name
		}
	}
	if mvpKills >= 1 {
		awards["MVP"] = mvpName
	}

	// Reaper: best K/D ratio (min 3 kills).
	var reaperName string
	bestKD := -1.0
	for _, bot := range bots {
		if bot.RoundKills < 3 {
			continue
		}
		var kd float64
		if bot.RoundDeaths > 0 {
			kd = float64(bot.RoundKills) / float64(bot.RoundDeaths)
		} else {
			kd = float64(bot.RoundKills)
		}
		if kd > bestKD {
			bestKD = kd
			reaperName = bot.Name
		}
	}
	if reaperName != "" {
		awards["Reaper"] = reaperName
	}

	// Unkillable: longest single life (min 1 tick).
	var unkillableName string
	bestLife := 0
	for _, bot := range bots {
		if bot.RoundLongestLife > bestLife {
			bestLife = bot.RoundLongestLife
			unkillableName = bot.Name
		}
	}
	if bestLife >= 1 {
		awards["Unkillable"] = unkillableName
	}

	// Speed Demon: most distance (min 1.0).
	var speedName string
	bestDist := 0.0
	for _, bot := range bots {
		if bot.RoundDistance > bestDist {
			bestDist = bot.RoundDistance
			speedName = bot.Name
		}
	}
	if bestDist >= 1.0 {
		awards["Speed Demon"] = speedName
	}

	// Sharpshooter: best hit rate (min 3 shots, ranged weapon: bow or staff).
	var sharpName string
	bestRate := -1.0
	for _, bot := range bots {
		if bot.Weapon != "bow" && bot.Weapon != "staff" {
			continue
		}
		if bot.RoundShotsFired < 3 {
			continue
		}
		rate := float64(bot.RoundShotsHit) / float64(bot.RoundShotsFired)
		if rate > bestRate {
			bestRate = rate
			sharpName = bot.Name
		}
	}
	if sharpName != "" {
		awards["Sharpshooter"] = sharpName
	}

	// Berserker: most damage dealt (min 1).
	var berserkerName string
	bestDmg := 0.0
	for _, bot := range bots {
		if bot.RoundDamageDealt > bestDmg {
			bestDmg = bot.RoundDamageDealt
			berserkerName = bot.Name
		}
	}
	if bestDmg >= 1.0 {
		awards["Berserker"] = berserkerName
	}

	return awards
}

// CalculateEloChange computes the Elo rating gain and loss for a kill event
// using the standard Elo formula with the configured K-factor.
func CalculateEloChange(killerElo, victimElo int) (gain, loss int) {
	kf := config.C.EloKFactor
	expected := 1.0 / (1.0 + math.Pow(10, float64(victimElo-killerElo)/400.0))

	gain = int(math.Round(kf * (1 - expected)))
	if gain < 1 {
		gain = 1
	}

	loss = int(math.Round(kf * expected))
	if loss < 1 {
		loss = 1
	}

	return gain, loss
}

// ApplyEloChange adjusts the Elo ratings of killer and victim after a kill.
// The victim's Elo will not drop below the configured minimum.
func ApplyEloChange(killer, victim *BotState) {
	gain, loss := CalculateEloChange(killer.Elo, victim.Elo)

	killer.Elo += gain

	victim.Elo -= loss
	if victim.Elo < config.C.EloMin {
		victim.Elo = config.C.EloMin
	}
}
