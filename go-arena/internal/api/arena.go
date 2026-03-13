package api

import (
	"net/http"

	"arena-server/internal/game"
)

// GetArenaStatus returns an http.HandlerFunc that serves GET /api/v1/arena/status.
// It reads the engine state under a read lock and returns a summary.
func GetArenaStatus(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := engine.GetArenaSnapshot()

		var status string
		switch snap.Phase {
		case game.PhaseLobby:
			status = "lobby"
		case game.PhaseActive:
			status = "active"
		case game.PhaseIntermission:
			status = "intermission"
		default:
			status = "unknown"
		}

		writeJSON(w, http.StatusOK, ArenaStatusResponse{
			Status:             status,
			BotsConnected:      snap.BotsConnected,
			BotsAlive:          snap.BotsAlive,
			RoundNumber:        snap.RoundNumber,
			RoundTimeRemaining: snap.RoundTimeRemaining,
			SafeZoneRadius:     snap.SafeZoneRadius,
			TopBot:             snap.TopBotName,
		})
	}
}
