package game

import (
	"encoding/json"
	"log/slog"

	"arena-server/internal/config"
)

// marshalJSON is a package-level helper that serialises v to JSON bytes.
func marshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// SendToBot marshals msg to JSON and sends it to the bot's write channel.
// The send is non-blocking: if the channel is full the message is silently
// dropped.
func SendToBot(bot *BotState, msg interface{}) {
	if bot.SendChan == nil {
		return
	}
	data, err := marshalJSON(msg)
	if err != nil {
		slog.Error("failed to marshal bot message", "bot_id", bot.BotID, "error", err)
		return
	}
	select {
	case bot.SendChan <- data:
	default:
		// Channel full; drop the message.
	}
}

// SendTickUpdate sends the per-tick game state update to a bot.
func SendTickUpdate(bot *BotState, yourState map[string]interface{}, nearbyEntities []map[string]interface{}, tickCount int) {
	msg := map[string]interface{}{
		"type":            "tick",
		"tick":            tickCount,
		"your_state":      yourState,
		"nearby_entities": nearbyEntities,
		"view_radius":     config.C.ViewRadius,
	}
	SendToBot(bot, msg)
}

// SendDeathMessage notifies a bot that it has died.
func SendDeathMessage(bot *BotState, event DeathEvent) {
	msg := map[string]interface{}{
		"type":                "death",
		"killed_by":           event.KillerID,
		"killer_name":         event.KillerName,
		"weapon_used":         event.Weapon,
		"damage":              event.Damage,
		"your_kills_this_life": event.VictimKills,
		"respawn_in_seconds":  config.C.RespawnTime,
	}
	SendToBot(bot, msg)
}

// SendKillMessage notifies a bot that it scored a kill.
func SendKillMessage(bot *BotState, event KillEvent) {
	msg := map[string]interface{}{
		"type":             "kill",
		"victim_name":      event.VictimName,
		"victim_id":        event.VictimID,
		"weapon_used":      event.Weapon,
		"your_kill_streak": event.KillStreak,
		"your_round_kills": event.RoundKills,
	}
	SendToBot(bot, msg)
}

// SendRespawnMessage notifies a bot that it has respawned.
func SendRespawnMessage(bot *BotState, event RespawnEvent) {
	msg := map[string]interface{}{
		"type":     "respawn",
		"position": event.Position,
		"hp":       event.HP,
	}
	SendToBot(bot, msg)
}

// SendRoundEnd notifies a bot that the round has ended.
func SendRoundEnd(bot *BotState, info RoundEndInfo, nextRoundIn float64) {
	msg := map[string]interface{}{
		"type":         "round_end",
		"round_number": info.RoundNumber,
		"your_stats": map[string]interface{}{
			"kills":  bot.RoundKills,
			"deaths": bot.RoundDeaths,
			"damage": bot.RoundDamageDealt,
		},
		"round_winner":  info.WinnerName,
		"next_round_in": nextRoundIn,
	}
	SendToBot(bot, msg)
}

// SendRoundStart notifies a bot that a new round has begun.
func SendRoundStart(bot *BotState, round RoundState, bots map[string]*BotState, obstacles []Obstacle, arena *ArenaMap) {
	allPositions := make(map[string]interface{}, len(bots))
	for id, b := range bots {
		allPositions[id] = b.Position
	}

	// Build obstacle list for serialisation.
	obsList := make([]map[string]interface{}, 0, len(obstacles))
	for _, obs := range obstacles {
		obsList = append(obsList, map[string]interface{}{
			"x":      obs.X,
			"y":      obs.Y,
			"width":  obs.Width,
			"height": obs.Height,
		})
	}

	msg := map[string]interface{}{
		"type":           "round_start",
		"round_number":   round.RoundNumber,
		"position":       bot.Position,
		"bots_in_round":  len(bots),
		"obstacles":      obsList,
		"all_positions":  allPositions,
		"safe_zone": map[string]interface{}{
			"center":        arena.ZoneCenter,
			"radius":        arena.ZoneRadius,
			"target_center": arena.ZoneTargetCenter,
			"target_radius": arena.ZoneTargetRadius,
		},
	}
	SendToBot(bot, msg)
}

// SendLobbyUpdate sends a lobby status message to a bot.
func SendLobbyUpdate(bot *BotState, connectedCount, minBots int, countdown *int, allBots map[string]*BotState) {
	players := make([]map[string]interface{}, 0, len(allBots))
	for _, b := range allBots {
		players = append(players, map[string]interface{}{
			"name":         b.Name,
			"avatar_color": b.AvatarColor,
			"weapon":       b.Weapon,
		})
	}

	var countdownVal interface{}
	if countdown != nil {
		countdownVal = *countdown
	}

	msg := map[string]interface{}{
		"type":           "lobby",
		"bots_connected": connectedCount,
		"bots_needed":    minBots,
		"countdown":      countdownVal,
		"players":        players,
	}
	SendToBot(bot, msg)
}

// BuildConnectedMessage returns the initial connection acknowledgement payload.
// Used by the bot handler to write directly before the writer goroutine starts.
func BuildConnectedMessage(bot *BotState, lastLoadout map[string]interface{}) map[string]interface{} {
	var loadout interface{}
	if lastLoadout != nil {
		loadout = lastLoadout
	}

	return map[string]interface{}{
		"type":              "connected",
		"bot_id":            bot.BotID,
		"arena_size":        [2]float64{config.C.ArenaWidth, config.C.ArenaHeight},
		"available_weapons": GetAvailableWeapons(),
		"stat_budget":       config.C.StatBudget,
		"stat_min":          config.C.StatMin,
		"stat_max":          config.C.StatMax,
		"timeout_seconds":   config.C.LoadoutTimeoutSecs,
		"last_loadout":      loadout,
	}
}

// SendConnectedMessage sends the initial connection acknowledgement to a bot.
func SendConnectedMessage(bot *BotState, lastLoadout map[string]interface{}) {
	SendToBot(bot, BuildConnectedMessage(bot, lastLoadout))
}

// SendLoadoutConfirmed confirms a bot's loadout selection with the derived
// stats.
// BuildLoadoutConfirmed returns the loadout_confirmed payload.
func BuildLoadoutConfirmed(bot *BotState, derived DerivedStats) map[string]interface{} {
	return map[string]interface{}{
		"type":   "loadout_confirmed",
		"weapon": bot.Weapon,
		"stats": map[string]interface{}{
			"hp":      bot.Stats["hp"],
			"speed":   bot.Stats["speed"],
			"attack":  bot.Stats["attack"],
			"defense": bot.Stats["defense"],
		},
		"computed": map[string]interface{}{
			"max_hp":           derived.MaxHP,
			"move_speed":       derived.MoveSpeed,
			"attack_mult":      derived.AttackMult,
			"defense_red":      derived.DefenseReduction,
			"attack_range":     derived.AttackRange,
			"cooldown_seconds": derived.CooldownSeconds,
			"weapon_damage":    derived.WeaponDamage,
		},
		"position": bot.Position,
	}
}

// SendLoadoutConfirmed confirms a bot's loadout selection with the derived stats.
func SendLoadoutConfirmed(bot *BotState, derived DerivedStats) {
	SendToBot(bot, BuildLoadoutConfirmed(bot, derived))
}

// BroadcastToSpectators sends pre-serialised data to every spectator
// connection. Sends are non-blocking.
func BroadcastToSpectators(spectators []*SpectatorConn, data []byte) {
	for _, s := range spectators {
		select {
		case s.SendChan <- data:
		default:
			// Channel full; drop for this spectator.
		}
	}
}

// SendError sends an error message to a bot.
func SendError(bot *BotState, message string) {
	msg := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	SendToBot(bot, msg)
}

// SendKick sends a kick message to a bot.
func SendKick(bot *BotState, reason string) {
	msg := map[string]interface{}{
		"type":   "kick",
		"reason": reason,
	}
	SendToBot(bot, msg)
}
