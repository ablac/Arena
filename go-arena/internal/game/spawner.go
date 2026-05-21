package game

import "arena-server/internal/config"

// SpawnBotAt places a bot at a specific position and resets its combat state.
// If the chosen cell is blocked it searches outward for the nearest open cell.
func SpawnBotAt(bot *BotState, pos Vec2, grid *SpatialGrid, tickCount int) {
	// Snap position to the nearest grid cell centre.
	if ActiveTerrain != nil {
		cell := ActiveTerrain.WorldToGrid(pos)
		// If the cell is a wall, spiral outward to find an unblocked cell.
		if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
			found := false
			for radius := 1; radius <= 10 && !found; radius++ {
				for dx := -radius; dx <= radius && !found; dx++ {
					for dy := -radius; dy <= radius && !found; dy++ {
						if dx != -radius && dx != radius && dy != -radius && dy != radius {
							continue // only check the ring perimeter
						}
						nc := [2]int{cell[0] + dx, cell[1] + dy}
						if !ActiveTerrain.IsBlocked(nc[0], nc[1]) {
							cell = nc
							found = true
						}
					}
				}
			}
		}
		pos = ActiveTerrain.GridToWorld(cell)
	}
	bot.Position = pos
	bot.LastValidPosition = pos
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

	bot.GrappleCharges = config.C.GrappleChargesPerRound
	bot.GrappleCooldown = 0
	bot.BountyTokenBonus = 0
	bot.TeleportHazardGraceTicks = 0
	bot.TeleportTouchedPads = make(map[string]bool)

	bot.RoundLifeStartTick = tickCount

	grid.Insert(bot.BotID, bot.Position)
}

// CheckDeaths finds all bots whose HP has reached zero, marks them dead,
// removes them from the spatial grid, and returns a DeathEvent for each.
func CheckDeaths(bots map[string]*BotState, grid *SpatialGrid) []DeathEvent {
	var events []DeathEvent

	for _, bot := range bots {
		if !bot.IsAlive || bot.HP > 0 {
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
