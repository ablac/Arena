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

// GetArenaMap returns an http.HandlerFunc that serves GET /api/v1/arena/map.
// It returns the current terrain grid, dimensions, cell size, teleport pads, and hazard zones.
// If no terrain is active (e.g. lobby before first round), returns a message.
func GetArenaMap(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		terrain := game.ActiveTerrain
		if terrain == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status":  "no_map",
				"message": "No terrain available yet. The next round's map is pre-generated during intermission — check back then.",
			})
			return
		}

		// Get teleport pads and hazard zones from engine
		pads, zones := engine.GetMapFeatures()

		// Also provide detailed metadata for bots that want extra info
		padViews := make([]map[string]interface{}, 0, len(pads))
		for _, pad := range pads {
			padViews = append(padViews, game.BuildTeleportPadView(pad, true))
		}

		zoneViews := make([]map[string]interface{}, 0, len(zones))
		for _, zone := range zones {
			zoneViews = append(zoneViews, game.BuildHazardZoneView(zone, true))
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":        "ok",
			"width":         terrain.Width,
			"height":        terrain.Height,
			"cell_size":     terrain.CellSize,
			"terrain":       terrain.ToCompactJSONWithFeatures(pads, zones),
			"teleport_pads": padViews,
			"hazard_zones":  zoneViews,
			"legend": map[string]string{
				".": "ground (passable)",
				"#": "wall (blocked)",
				"T": "teleport pad",
				"H": "hazard zone (damage when active)",
			},
		})
	}
}
