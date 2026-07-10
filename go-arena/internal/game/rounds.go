package game

import (
	"math"

	"arena-server/internal/config"
)

// ShouldEndRound returns true when the current round should end, based on
// time expiry, bot count, all bots being dead/disconnected, or the active
// game mode's win condition (team elimination, CTF capture target).
func ShouldEndRound(bots map[string]*BotState, round *RoundState, tickCount int, teamScores map[int]int) bool {
	c := &config.C

	// Duration exceeded.
	if (tickCount - round.StartTick) >= int(c.RoundDuration*float64(c.TickRate)) {
		return true
	}

	// All bots disconnected.
	if len(bots) == 0 {
		return true
	}

	rules := ActiveModeRules
	if rules.HasTeams() {
		// CTF: a team reached the capture target.
		if rules.UsesFlags && CTFWinningTeam(teamScores) != 0 {
			return true
		}
		// Team elimination: at most one team still has alive bots.
		if rules.TeamElimination && len(bots) >= c.MinBotsToStart {
			if len(AliveTeams(bots)) <= 1 {
				return true
			}
		}
	} else {
		// FFA: at most 1 bot alive and round started with >= 2.
		alive := 0
		for _, bot := range bots {
			if bot.IsAlive {
				alive++
			}
		}
		if alive <= 1 && len(bots) >= c.MinBotsToStart {
			return true
		}
	}

	// At most 1 bot connected.
	if len(bots) <= 1 {
		return true
	}

	return false
}

// DetermineWinner returns the ID and name of the round winner.
// FFA priority: last bot alive, then most kills, then first found.
// Team modes: the winning team is resolved first (CTF score, then last team
// standing, then total kills) and its best member is credited as winner.
func DetermineWinner(bots map[string]*BotState, teamScores map[int]int) (winnerID, winnerName string) {
	rules := ActiveModeRules
	if rules.HasTeams() {
		if team := DetermineWinningTeam(bots, teamScores); team != 0 {
			return bestBotOnTeam(bots, team)
		}
	}

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

// DetermineWinningTeam resolves the winning team for team modes: highest CTF
// score first, then the last team with alive bots, then most total kills.
// Returns 0 when no team can be singled out.
func DetermineWinningTeam(bots map[string]*BotState, teamScores map[int]int) int {
	// CTF score.
	bestTeam, bestScore := 0, 0
	tied := false
	for team, score := range teamScores {
		if score > bestScore {
			bestTeam, bestScore = team, score
			tied = false
		} else if score == bestScore && score > 0 {
			tied = true
		}
	}
	if bestTeam != 0 && bestScore > 0 && !tied {
		return bestTeam
	}

	// Last team standing.
	alive := AliveTeams(bots)
	if len(alive) == 1 {
		for team := range alive {
			return team
		}
	}

	// Most total kills.
	kills := make(map[int]int)
	for _, bot := range bots {
		if bot.Team > 0 {
			kills[bot.Team] += bot.RoundKills
		}
	}
	bestTeam, bestKills := 0, -1
	tied = false
	for team, k := range kills {
		if k > bestKills {
			bestTeam, bestKills = team, k
			tied = false
		} else if k == bestKills {
			tied = true
		}
	}
	if tied {
		return 0
	}
	return bestTeam
}

// bestBotOnTeam picks the winning team's top performer (flag captures first,
// then kills, then damage) so individual ELO/award flows keep working.
func bestBotOnTeam(bots map[string]*BotState, team int) (winnerID, winnerName string) {
	var best *BotState
	for _, bot := range bots {
		if bot.Team != team {
			continue
		}
		if best == nil ||
			bot.RoundFlagCaptures > best.RoundFlagCaptures ||
			(bot.RoundFlagCaptures == best.RoundFlagCaptures && bot.RoundKills > best.RoundKills) ||
			(bot.RoundFlagCaptures == best.RoundFlagCaptures && bot.RoundKills == best.RoundKills && bot.RoundDamageDealt > best.RoundDamageDealt) {
			best = bot
		}
	}
	if best == nil {
		return "", ""
	}
	return best.BotID, best.Name
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
	if kf <= 0 {
		return 0, 0
	}
	expected := 1.0 / (1.0 + math.Pow(10, float64(victimElo-killerElo)/400.0))
	delta := int(math.Round(kf * (1 - expected)))
	if delta < 0 {
		delta = 0
	}
	return delta, delta
}

// ClampElo contains legacy inflated values and keeps all non-kill rating
// rewards inside the same published bounds.
func ClampElo(elo int) int {
	return config.ClampElo(elo)
}

// ApplyEloChange performs one matched transfer. If either player is already
// at a bound, the transfer shrinks rather than creating or destroying points.
func ApplyEloChange(killer, victim *BotState) {
	if killer == nil || victim == nil || killer == victim {
		return
	}
	killer.Elo = ClampElo(killer.Elo)
	victim.Elo = ClampElo(victim.Elo)
	delta, _ := CalculateEloChange(killer.Elo, victim.Elo)
	minElo, maxElo := config.EloBounds()
	if available := victim.Elo - minElo; delta > available {
		delta = available
	}
	if capacity := maxElo - killer.Elo; delta > capacity {
		delta = capacity
	}
	if delta <= 0 {
		return
	}
	killer.Elo += delta
	victim.Elo -= delta
}
