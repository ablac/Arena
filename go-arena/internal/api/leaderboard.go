package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"arena-server/internal/db"
)

// GetLeaderboard handles GET /api/v1/leaderboard.
// It accepts optional query parameters: sort, limit, and offset.
func GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	// Parse sort parameter.
	sortBy := r.URL.Query().Get("sort")
	switch sortBy {
	case "kills", "elo", "streak", "kd_ratio":
		if sortBy == "streak" {
			sortBy = "best_streak"
		}
	default:
		sortBy = "elo"
	}

	// Parse limit (1-100, default 50).
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

	// Parse offset (>= 0, default 0).
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}

	if db.Pool == nil {
		writeJSON(w, http.StatusOK, LeaderboardResponse{
			Entries: []LeaderboardEntry{},
			Total:   0,
			Limit:   limit,
			Offset:  offset,
		})
		return
	}

	ctx := r.Context()

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

	// Map DB entries to API entries.
	apiEntries := make([]LeaderboardEntry, 0, len(entries))
	for _, e := range entries {
		apiEntries = append(apiEntries, LeaderboardEntry{
			Rank:         e.Rank,
			BotID:        e.BotID,
			Name:         e.Name,
			AvatarColor:  e.AvatarColor,
			Kills:        e.Kills,
			Deaths:       e.Deaths,
			Elo:          e.Elo,
			BestStreak:   e.BestStreak,
			DamageDealt:  e.DamageDealt,
			RoundsPlayed: e.RoundsPlayed,
			RoundWins:    e.RoundWins,
		})
	}

	writeJSON(w, http.StatusOK, LeaderboardResponse{
		Entries: apiEntries,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	})
}
