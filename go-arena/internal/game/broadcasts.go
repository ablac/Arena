package game

import (
	"encoding/json"
	"log/slog"
	"math"
	"sort"

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
	safeSend(bot.SendChan, data)
}

// SendMapInit sends the static terrain grid to a bot at the start of a round.
// DEPRECATED: No longer called — bots should use GET /api/v1/arena/map instead.
// The next round's terrain is pre-generated during intermission.
// Kept for reference / potential future use.
func SendMapInit(bot *BotState, terrain *TerrainGrid) {
	msg := map[string]interface{}{
		"type":      "map_init",
		"width":     terrain.Width,
		"height":    terrain.Height,
		"cell_size": terrain.CellSize,
		"terrain":   terrain.ToCompactJSON(),
		"legend": map[string]string{
			"V": "void",
			".": "ground",
			"#": "wall",
			"~": "water",
		},
	}
	SendToBot(bot, msg)
}

// SendTickUpdate sends the per-tick game state update to a bot.
// hints is optional — when non-nil it provides directional hints to far-away
// bots and pickups (only sent when no bots are within view radius).
func SendTickUpdate(bot *BotState, yourState map[string]interface{}, nearbyEntities []map[string]interface{}, tickCount int, arena *ArenaMap, hints []map[string]interface{}, fogRadius int, extra ...map[string]interface{}) {
	var cellSize float64 = config.C.PathfindingCellSize
	zoneCenter := posToGrid(arena.ZoneCenter)
	zoneTargetCenter := posToGrid(arena.ZoneTargetCenter)

	msg := map[string]interface{}{
		"type":            "tick",
		"tick":            tickCount,
		"tick_number":     tickCount,
		"your_state":      yourState,
		"nearby_entities": nearbyEntities,
		"fog_radius":      fogRadius,
		"safe_zone": map[string]interface{}{
			"center":        [2]int{zoneCenter[0], zoneCenter[1]},
			"radius":        int(math.Round(arena.ZoneRadius / cellSize)),
			"target_center": [2]int{zoneTargetCenter[0], zoneTargetCenter[1]},
			"target_radius": int(math.Round(arena.ZoneTargetRadius / cellSize)),
		},
	}
	if hints != nil {
		msg["hints"] = hints
	}
	// Merge extra data into the tick message.
	for _, ext := range extra {
		for k, v := range ext {
			msg[k] = v
		}
	}
	SendToBot(bot, msg)
}

// SendDeathMessage notifies a bot that it has died.
func SendDeathMessage(bot *BotState, event DeathEvent) {
	msg := map[string]interface{}{
		"type":                 "death",
		"killed_by":            event.KillerID,
		"killer_name":          event.KillerName,
		"weapon_used":          event.Weapon,
		"damage":               event.Damage,
		"your_kills_this_life": event.VictimKills,
		"respawn":              false,
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
		"damage":           event.Damage,
		"your_kill_streak": event.KillStreak,
		"your_round_kills": event.RoundKills,
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
// Terrain is available via GET /api/v1/arena/map (pre-generated during intermission).
func SendRoundStart(bot *BotState, round RoundState, bots map[string]*BotState, arena *ArenaMap) {
	gridPos := posToGrid(bot.Position)
	cellSize := config.C.PathfindingCellSize
	zoneCenter := posToGrid(arena.ZoneCenter)
	zoneTargetCenter := posToGrid(arena.ZoneTargetCenter)

	msg := map[string]interface{}{
		"type":                 "round_start",
		"round_number":         round.RoundNumber,
		"round_modifier":       string(round.Modifier),
		"round_modifier_label": round.Modifier.Label(),
		"position":             [2]int{gridPos[0], gridPos[1]},
		"bots_in_round":        len(bots),
		// Older clients expect this object to exist. Expose only the receiver's
		// already-known position; restoring opponent entries would recreate the
		// round-start radar leak that the fairness boundary removed.
		"all_positions": map[string]interface{}{
			bot.BotID: [2]int{gridPos[0], gridPos[1]},
		},
		"safe_zone": map[string]interface{}{
			"center":        [2]int{zoneCenter[0], zoneCenter[1]},
			"radius":        int(math.Round(arena.ZoneRadius / cellSize)),
			"target_center": [2]int{zoneTargetCenter[0], zoneTargetCenter[1]},
			"target_radius": int(math.Round(arena.ZoneTargetRadius / cellSize)),
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
	sort.Slice(players, func(i, j int) bool {
		return players[i]["name"].(string) < players[j]["name"].(string)
	})

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

	gridW := int(config.C.ArenaWidth / config.C.PathfindingCellSize)
	gridH := int(config.C.ArenaHeight / config.C.PathfindingCellSize)

	return map[string]interface{}{
		"type":              "connected",
		"bot_id":            bot.BotID,
		"arena_size":        [2]float64{config.C.ArenaWidth, config.C.ArenaHeight},
		"grid_size":         [2]int{gridW, gridH},
		"cell_size":         config.C.PathfindingCellSize,
		"fog_radius":        config.C.FogRadius,
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
// connection. Sends are non-blocking. Safe against closed channels
// (spectator may disconnect between snapshot and send).
func BroadcastToSpectators(spectators []*SpectatorConn, data []byte) {
	for _, s := range spectators {
		safeSend(s.SendChan, data)
	}
}

// safeSend performs a non-blocking send on ch, recovering gracefully if
// the channel has been closed (e.g. spectator disconnected).
func safeSend(ch chan []byte, data []byte) {
	defer func() { recover() }()
	select {
	case ch <- data:
	default:
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

// SendStructuredError sends a structured error message with code and details.
func SendStructuredError(bot *BotState, message, code string, details map[string]interface{}) {
	msg := map[string]interface{}{
		"type":    "error",
		"message": message,
		"code":    code,
	}
	if details != nil {
		msg["details"] = details
	}
	SendToBot(bot, msg)
}

// SendKick sends a kick message to a bot with the reason.
func SendKick(bot *BotState, reason string) {
	msg := map[string]interface{}{
		"type":   "kick",
		"reason": reason,
	}
	SendToBot(bot, msg)
}
