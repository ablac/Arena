package game

import "arena-server/internal/config"

// SpawnBotAt places a bot at a specific position and resets its combat state.
// If the chosen cell is blocked it searches outward for the nearest open cell.
func SpawnBotAt(bot *BotState, pos Vec2, grid *SpatialGrid, tickCount int) {
	// Snap position to the nearest grid cell centre. If the cell is a wall,
	// take the nearest open cell anywhere on the grid — a bounded search
	// (formerly radius 10) could give up inside thick caves walls and leave
	// the bot permanently embedded.
	if ActiveTerrain != nil {
		cell := ActiveTerrain.WorldToGrid(pos)
		if open, ok := ActiveTerrain.NearestOpenCell(cell, 0); ok {
			cell = open
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
	bot.MovementTrace = nil

	bot.GrappleCharges = config.C.GrappleChargesPerRound
	bot.GrappleCooldown = 0
	bot.BountyTokenBonus = 0
	bot.TeleportHazardGraceTicks = 0
	bot.TeleportTouchedPads = make(map[string]bool)

	bot.RoundLifeStartTick = tickCount
	bot.ResetDamageAttribution()

	grid.Insert(bot.BotID, bot.Position)
}

const killCreditTTLSeconds = 5

func killCreditTTLTicks() int {
	tickRate := config.C.TickRate
	if tickRate <= 0 {
		tickRate = 10
	}
	return killCreditTTLSeconds * tickRate
}

func recentDamageKiller(victim *BotState, bots map[string]*BotState, tickCount int) *BotState {
	if victim == nil || victim.LastDamagedBy == "" || victim.LastDamagedBy == victim.BotID {
		return nil
	}

	age := tickCount - victim.LastDamageTick
	if age < 0 || age > killCreditTTLTicks() {
		return nil
	}

	return bots[victim.LastDamagedBy]
}

// CheckDeaths finds all bots whose HP has reached zero, marks them dead,
// removes them from the spatial grid, and returns a DeathEvent for each.
func CheckDeaths(bots map[string]*BotState, grid *SpatialGrid, tickCount int) []DeathEvent {
	var events []DeathEvent

	for _, bot := range bots {
		if !bot.IsAlive || bot.HP > 0 {
			continue
		}

		bot.IsAlive = false
		bot.HP = 0
		grid.Remove(bot.BotID)

		death := DeathEvent{
			VictimID:    bot.BotID,
			VictimKills: bot.RoundKills,
		}
		if killer := recentDamageKiller(bot, bots, tickCount); killer != nil {
			death.KillerID = killer.BotID
			death.KillerName = killer.Name
			death.Weapon = bot.LastDamageSource
			death.Damage = bot.LastDamageAmount
		}
		events = append(events, death)

		bot.RoundDeaths++
	}

	return events
}

// No respawns - dead bots stay dead until next round.
