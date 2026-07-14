package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"arena-server/internal/db"
)

// GetLeaderboard handles GET /api/v1/leaderboard.
// Query params: sort, limit, offset, period (all_time|30d|7d|24h|1h).
func GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "kills", "elo", "streak", "kd_ratio", "wins", "damage":
		if sortBy == "streak" {
			sortBy = "best_streak"
		}
	default:
		sortBy = "elo"
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 100 {
				n = 100
			}
			limit = n
		}
	}

	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}

	if db.Pool == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"entries": []interface{}{}, "total": 0, "limit": limit, "offset": offset, "period": "all_time",
		})
		return
	}

	ctx := r.Context()
	period := r.URL.Query().Get("period")

	// Time-based leaderboard
	if period != "" && period != "all_time" {
		var since time.Time
		switch period {
		case "1h":
			since = time.Now().Add(-1 * time.Hour)
		case "24h":
			since = time.Now().Add(-24 * time.Hour)
		case "7d":
			since = time.Now().Add(-7 * 24 * time.Hour)
		case "30d":
			since = time.Now().Add(-30 * 24 * time.Hour)
		default:
			since = time.Now().Add(-24 * time.Hour)
			period = "24h"
		}

		entries, err := db.GetTimeBasedLeaderboard(ctx, since, sortBy, limit)
		if err != nil {
			slog.Error("failed to get time-based leaderboard", "error", err, "period", period)
			writeError(w, http.StatusInternalServerError, "failed to get leaderboard")
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"entries": entries,
			"total":   len(entries),
			"limit":   limit,
			"offset":  0,
			"period":  period,
		})
		return
	}

	// All-time leaderboard (existing behavior)
	entries, err := db.GetLeaderboard(ctx, sortBy, limit, offset)
	if err != nil {
		slog.Error("failed to get leaderboard", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get leaderboard")
		return
	}

	total, err := db.GetLeaderboardCount(ctx)
	if err != nil {
		slog.Error("failed to get leaderboard count", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get leaderboard count")
		return
	}

	apiEntries := make([]LeaderboardEntry, 0, len(entries))
	for _, e := range entries {
		apiEntries = append(apiEntries, LeaderboardEntry{
			Rank: e.Rank, BotID: e.BotID, Name: e.Name, AvatarColor: e.AvatarColor,
			Kills: e.Kills, Deaths: e.Deaths, Elo: e.Elo, BestStreak: e.BestStreak,
			DamageDealt: e.DamageDealt, RoundsPlayed: e.RoundsPlayed, RoundWins: e.RoundWins,
		})
	}

	writeJSON(w, http.StatusOK, LeaderboardResponse{
		Entries: apiEntries, Total: total, Limit: limit, Offset: offset,
	})
}
