package api

import (
	"encoding/json"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Apply name.
	if req.Name != nil {
		bot.Name = security.SanitizeBotName(*req.Name)
	}

	// Apply avatar color.
	if req.AvatarColor != nil {
		if !security.ValidateColor(*req.AvatarColor) {
			writeError(w, http.StatusBadRequest, "invalid avatar_color: must be a hex color like #ff00aa")
			return
		}
		bot.AvatarColor = *req.AvatarColor
	}

	// Apply loadout.
	if req.DefaultLoadout != nil {
		lo := req.DefaultLoadout

		if !security.ValidateWeapon(lo.Weapon) {
			writeError(w, http.StatusBadRequest, "invalid weapon")
			return
		}
		bot.DefaultWeapon = lo.Weapon

		if err := security.ValidateStats(lo.Stats); err != nil {
			writeError(w, http.StatusBadRequest, "invalid stats: "+err.Error())
			return
		}
		bot.DefaultStats = db.JSONBStats(lo.Stats)

		if !security.ValidateFallbackBehavior(lo.Fallback) {
			writeError(w, http.StatusBadRequest, "invalid fallback_behavior")
			return
		}
		bot.DefaultFallback = lo.Fallback
	}

	bot.UpdatedAt = time.Now()

	if db.Pool != nil {
		if err := db.UpdateBot(r.Context(), bot); err != nil {
			slog.Error("failed to update bot", "error", err, "bot_id", bot.ID)
			writeError(w, http.StatusInternalServerError, "failed to update bot")
			return
		}
	}

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
