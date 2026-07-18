package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"arena-server/internal/db"
)

// The standings UI polls /leaderboard every 15s per open viewer (30s on
// mobile) with no jitter, and every poll previously ran two uncached
// aggregate queries. Cache the encoded response per (sort, period, limit) for
// one poll interval so DB load stays constant in viewer count, and serve 304s
// via ETag. Only the first page at the two shipped page sizes is cached —
// arbitrary offset/limit combinations bypass the cache so the key space stays
// bounded (6 sorts x 5 periods x 2 limits).
const (
	leaderboardCacheTTL    = 15 * time.Second
	leaderboardLoadTimeout = 10 * time.Second
)

var leaderboardCache = newResponseCache(leaderboardCacheTTL, leaderboardLoadTimeout, time.Now)

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

	// Normalize period before it becomes part of a cache key: unknown values
	// collapse to 24h (preserving the pre-cache fallback behavior).
	period := r.URL.Query().Get("period")
	switch period {
	case "", "all_time":
		period = "all_time"
	case "1h", "24h", "7d", "30d":
	default:
		period = "24h"
	}

	if db.Pool == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"entries": []interface{}{}, "total": 0, "limit": limit, "offset": offset, "period": "all_time",
		})
		return
	}

	if offset == 0 && (limit == 20 || limit == 50) {
		key := sortBy + "|" + period + "|" + strconv.Itoa(limit)
		leaderboardCache.Serve(w, r, key, func(ctx context.Context) ([]byte, error) {
			return buildLeaderboardBody(ctx, sortBy, period, limit, 0)
		}, "failed to get leaderboard")
		return
	}

	body, err := buildLeaderboardBody(r.Context(), sortBy, period, limit, offset)
	if err != nil {
		slog.Error("failed to get leaderboard", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get leaderboard")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write leaderboard response", "error", err)
	}
}

// buildLeaderboardBody runs the underlying queries and returns the encoded
// JSON response for the given normalized parameters.
func buildLeaderboardBody(ctx context.Context, sortBy, period string, limit, offset int) ([]byte, error) {
	// Time-based leaderboard.
	if period != "all_time" {
		var since time.Time
		switch period {
		case "1h":
			since = time.Now().Add(-1 * time.Hour)
		case "7d":
			since = time.Now().Add(-7 * 24 * time.Hour)
		case "30d":
			since = time.Now().Add(-30 * 24 * time.Hour)
		default:
			since = time.Now().Add(-24 * time.Hour)
		}

		entries, err := db.GetTimeBasedLeaderboard(ctx, since, sortBy, limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]interface{}{
			"entries": entries,
			"total":   len(entries),
			"limit":   limit,
			"offset":  0,
			"period":  period,
		})
	}

	// All-time leaderboard.
	entries, err := db.GetLeaderboard(ctx, sortBy, limit, offset)
	if err != nil {
		return nil, err
	}
	total, err := db.GetLeaderboardCount(ctx)
	if err != nil {
		return nil, err
	}

	apiEntries := make([]LeaderboardEntry, 0, len(entries))
	for _, e := range entries {
		apiEntries = append(apiEntries, LeaderboardEntry{
			Rank: e.Rank, BotID: e.BotID, Name: e.Name, AvatarColor: e.AvatarColor,
			Kills: e.Kills, Deaths: e.Deaths, Elo: e.Elo, BestStreak: e.BestStreak,
			DamageDealt: e.DamageDealt, RoundsPlayed: e.RoundsPlayed, RoundWins: e.RoundWins,
		})
	}
	return json.Marshal(LeaderboardResponse{
		Entries: apiEntries, Total: total, Limit: limit, Offset: offset,
	})
}
