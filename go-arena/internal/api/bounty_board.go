package api

import (
	"net/http"

	"arena-server/internal/game"
)

// GetBountyBoard handles GET /api/v1/bounties.
func GetBountyBoard(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		board := engine.GetBountyBoard()
		entries := make([]BountyBoardEntry, 0, len(board))
		for i, entry := range board {
			entries = append(entries, BountyBoardEntry{
				Rank:         i + 1,
				BotID:        entry.BotID,
				Name:         entry.Name,
				AvatarColor:  entry.AvatarColor,
				Weapon:       entry.Weapon,
				BountyPoints: entry.BountyPoints,
				WinStreak:    entry.WinStreak,
				Claims:       entry.Claims,
				IsTarget:     entry.IsTarget,
			})
		}

		writeJSON(w, http.StatusOK, BountyBoardResponse{
			Entries: entries,
			Total:   len(entries),
		})
	}
}
