package game

// SpawnBotAt places a bot at a specific position and resets its combat state.
func SpawnBotAt(bot *BotState, pos Vec2, grid *SpatialGrid, tickCount int) {
	bot.Position = pos
	bot.HP = bot.MaxHP
	bot.IsAlive = true
	bot.CooldownRemaining = 0

	bot.ActiveEffects = nil
	bot.DodgeCooldown = 0
	bot.InvulnTicks = 0
	bot.StunTicks = 0
	bot.ShieldAbsorb = 0

	bot.CurrentPath = nil
	bot.PathTarget = nil

	bot.RoundLifeStartTick = tickCount

	grid.Insert(bot.BotID, bot.Position)
}

// CheckDeaths finds all bots whose HP has reached zero, marks them dead,
// removes them from the spatial grid, and returns a DeathEvent for each.
func CheckDeaths(bots map[string]*BotState, grid *SpatialGrid) []DeathEvent {
	var events []DeathEvent

	for _, bot := range bots {
		if !bot.IsAlive || bot.HP >= 1 {
			continue
		}

		bot.IsAlive = false
		bot.HP = 0
		grid.Remove(bot.BotID)

		events = append(events, DeathEvent{
			VictimID:    bot.BotID,
			KillerID:    bot.LastDamagedBy,
			VictimKills: bot.RoundKills,
		})

		bot.RoundDeaths++
	}

	return events
}

// No respawns - dead bots stay dead until next round.
