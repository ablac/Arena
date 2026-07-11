package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

// UpdateBotConfig handles PUT /api/v1/bot/config.
// It validates the incoming configuration and persists it to the database.
func UpdateBotConfig(w http.ResponseWriter, r *http.Request) {
	bot := security.GetBotFromContext(r.Context())
	if bot == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req BotConfigRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Build and validate a complete candidate before changing the authenticated
	// bot record. A rejected field must not leave earlier fields applied.
	updatedBot := *bot

	// Apply name.
	if req.Name != nil {
		updatedBot.Name = security.SanitizeBotName(*req.Name)
	}

	// Apply avatar color.
	if req.AvatarColor != nil {
		if !security.ValidateColor(*req.AvatarColor) {
			writeError(w, http.StatusBadRequest, "invalid avatar_color: must be a hex color like #ff00aa")
			return
		}
		updatedBot.AvatarColor = *req.AvatarColor
	}

	// Apply loadout.
	if req.DefaultLoadout != nil {
		lo := req.DefaultLoadout

		if !security.ValidateWeapon(lo.Weapon) {
			writeError(w, http.StatusBadRequest, "invalid weapon")
			return
		}
		updatedBot.DefaultWeapon = lo.Weapon

		if err := security.ValidateStats(lo.Stats); err != nil {
			writeError(w, http.StatusBadRequest, "invalid stats: "+err.Error())
			return
		}
		updatedBot.DefaultStats = db.JSONBStats(lo.Stats)

		if !security.ValidateFallbackBehavior(lo.Fallback) {
			writeError(w, http.StatusBadRequest, "invalid fallback_behavior")
			return
		}
		updatedBot.DefaultFallback = lo.Fallback
	}

	updatedBot.UpdatedAt = time.Now()

	if db.Pool != nil {
		if err := db.UpdateBot(r.Context(), &updatedBot); err != nil {
			slog.Error("failed to update bot", "error", err, "bot_id", bot.ID)
			writeError(w, http.StatusInternalServerError, "failed to update bot")
			return
		}
	}
	*bot = updatedBot

	writeJSON(w, http.StatusOK, BotConfigResponse{
		BotID:       bot.ID,
		Name:        bot.Name,
		AvatarColor: bot.AvatarColor,
		Weapon:      bot.DefaultWeapon,
		Stats:       map[string]int(bot.DefaultStats),
		Fallback:    bot.DefaultFallback,
		UpdatedAt:   bot.UpdatedAt,
	})
}

// GetBotStats returns an http.HandlerFunc that serves GET /api/v1/bot/stats.
// It reads lifetime stats from the database and returns them alongside the
// bot's current rank.
func GetBotStats(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bot := security.GetBotFromContext(r.Context())
		if bot == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		ctx := r.Context()

		// Default response when DB is unavailable.
		resp := BotStatsResponse{
			BotID: bot.ID,
			Name:  bot.Name,
			Elo:   1000,
		}

		if db.Pool != nil {
			stats, err := db.GetBotStats(ctx, bot.ID)
			if err != nil {
				slog.Error("failed to get bot stats", "error", err, "bot_id", bot.ID)
				writeError(w, http.StatusInternalServerError, "failed to get bot stats")
				return
			}

			if stats != nil {
				var kdRatio float64
				if stats.Deaths > 0 {
					kdRatio = float64(stats.Kills) / float64(stats.Deaths)
				} else {
					kdRatio = float64(stats.Kills)
				}

				rank, err := db.GetBotRank(ctx, bot.ID, "elo")
				if err != nil {
					slog.Warn("failed to get bot rank", "error", err, "bot_id", bot.ID)
				}

				resp = BotStatsResponse{
					BotID:            bot.ID,
					Name:             bot.Name,
					Kills:            stats.Kills,
					Deaths:           stats.Deaths,
					KDRatio:          kdRatio,
					Assists:          stats.Assists,
					DamageDealt:      stats.DamageDealt,
					DamageTaken:      stats.DamageTaken,
					CurrentStreak:    stats.CurrentStreak,
					BestStreak:       stats.BestStreak,
					Elo:              stats.Elo,
					Rank:             rank,
					RoundsPlayed:     stats.RoundsPlayed,
					RoundWins:        stats.RoundWins,
					PickupsCollected: stats.PickupsCollected,
					DistanceTraveled: stats.DistanceTraveled,
					TimeAliveSeconds: stats.TimeAliveSecs,
					LongestLifeSecs:  stats.LongestLifeSecs,
				}
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// GetBotLive returns real-time game state for the authenticated bot.
// Returns whether the bot is connected, its current HP, position, action, etc.
func GetBotLive(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bot := security.GetBotFromContext(r.Context())
		if bot == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		// Check if bot is currently in the game
		detail, ok := engine.GetBotDetail(bot.ID)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"online":  false,
				"bot_id":  bot.ID,
				"name":    bot.Name,
				"message": "Bot is not currently connected to the arena",
			})
			return
		}

		// Get arena status for context
		gs := engine.GetFullGameState()
		phase, _ := gs["round_phase"].(string)

		// Build radar metrics (0-100 scale for visualization)
		roundKills, _ := detail["round_kills"].(int)
		roundDeaths, _ := detail["round_deaths"].(int)
		dmgDealt, _ := detail["round_damage_dealt"].(float64)
		dmgTaken, _ := detail["round_damage_taken"].(float64)
		shotsFired, _ := detail["round_shots_fired"].(int)
		shotsHit, _ := detail["round_shots_hit"].(int)
		distance, _ := detail["round_distance"].(float64)
		pickups, _ := detail["round_pickups"].(int)
		hp, _ := detail["hp"].(float64)
		maxHP, _ := detail["max_hp"].(float64)
		speed, _ := detail["speed"].(float64)
		atkMult, _ := detail["attack_multiplier"].(float64)
		defRed, _ := detail["defense_reduction"].(float64)
		killStreak, _ := detail["kill_streak"].(int)

		var accuracy float64
		if shotsFired > 0 {
			accuracy = float64(shotsHit) / float64(shotsFired) * 100
		}
		var dmgRatio float64
		if dmgTaken > 0 {
			dmgRatio = dmgDealt / dmgTaken
		} else if dmgDealt > 0 {
			dmgRatio = 10
		}

		// Action distribution from history
		actionCounts := detail["action_counts"]

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"online":             true,
			"bot_id":             bot.ID,
			"name":               bot.Name,
			"phase":              phase,
			"hp":                 hp,
			"max_hp":             maxHP,
			"position":           detail["position"],
			"weapon":             detail["weapon"],
			"is_alive":           detail["is_alive"],
			"speed":              speed,
			"attack_mult":        atkMult,
			"defense_red":        defRed,
			"kill_streak":        killStreak,
			"round_kills":        roundKills,
			"round_deaths":       roundDeaths,
			"round_damage_dealt": dmgDealt,
			"round_damage_taken": dmgTaken,
			"round_shots_fired":  shotsFired,
			"round_shots_hit":    shotsHit,
			"round_distance":     distance,
			"round_pickups":      pickups,
			"accuracy":           accuracy,
			"damage_ratio":       dmgRatio,
			"active_effects":     detail["active_effects"],
			"dodge_cooldown":     detail["dodge_cooldown"],
			"cooldown_remaining": detail["cooldown_remaining"],
			"current_action":     detail["current_action"],
			"last_action_result": detail["last_action_result"],
			"frozen":             detail["frozen"],
			"action_counts":      actionCounts,
		})
	}
}
