package game

import "arena-server/internal/config"

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

// SpawnBot places a bot at a random safe-zone position and resets its combat
// state for a fresh life.
func SpawnBot(bot *BotState, arena *ArenaMap, grid *SpatialGrid, tickCount int) {
	bot.Position = arena.GetSpawnPoint()
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

// RespawnTimer tracks the remaining time before a dead bot respawns.
type RespawnTimer struct {
	BotID     string
	Remaining float64
}

// ProcessRespawns decrements each active respawn timer and spawns bots whose
// timers have expired. Returns a RespawnEvent for each bot that respawned.
func ProcessRespawns(timers *[]RespawnTimer, bots map[string]*BotState, arena *ArenaMap, grid *SpatialGrid, dt float64, tickCount int) []RespawnEvent {
	var events []RespawnEvent

	// Iterate backwards so removals are safe.
	for i := len(*timers) - 1; i >= 0; i-- {
		(*timers)[i].Remaining -= dt
		if (*timers)[i].Remaining > 0 {
			continue
		}

		botID := (*timers)[i].BotID
		bot, ok := bots[botID]
		if ok {
			SpawnBot(bot, arena, grid, tickCount)
			events = append(events, RespawnEvent{
				BotID:    botID,
				Position: bot.Position,
				HP:       bot.HP,
			})
		}

		// Remove expired timer.
		*timers = append((*timers)[:i], (*timers)[i+1:]...)
	}

	return events
}

// AddRespawnTimer enqueues a respawn timer for the given bot using the
// configured respawn delay.
func AddRespawnTimer(timers *[]RespawnTimer, botID string) {
	*timers = append(*timers, RespawnTimer{
		BotID:     botID,
		Remaining: config.C.RespawnTime,
	})
}
