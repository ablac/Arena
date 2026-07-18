package api

import (
	"net/http"
	"sync"

	"arena-server/internal/game"
)

// arenaMapTerrainCacheKey identifies one round's compact-terrain encoding.
// The terrain pointer swaps at exactly the two round-transition sites
// (startRound/endRound) and FeaturesPending flips once when round features
// spawn, so key inequality is a complete invalidation signal. Feature-view
// maps stay per-request: they embed tick-dependent cooldown fields.
type arenaMapTerrainCacheKey struct {
	terrain         *game.TerrainGrid
	featuresPending bool
	pads            int
	zones           int
	capturePads     int
}

var (
	arenaMapTerrainMu        sync.Mutex
	arenaMapTerrainCachedKey arenaMapTerrainCacheKey
	arenaMapTerrainRows      []string
)

// compactTerrainRows memoizes ToCompactJSONWithFeatures (a 10k-cell copy plus
// 100 row-string allocations) for the round. Every demo bot fetches
// /arena/map at connect, round_start, and round_end in the same second, so
// without this the server rebuilt the identical encoding N times per round
// boundary. Built under the mutex on purpose: a burst should coalesce into
// one build, and the build is sub-millisecond.
func compactTerrainRows(terrain *game.TerrainGrid, featuresPending bool, pads []game.TeleportPad, zones []game.HazardZone, capturePads []game.CapturePad) []string {
	key := arenaMapTerrainCacheKey{terrain, featuresPending, len(pads), len(zones), len(capturePads)}
	arenaMapTerrainMu.Lock()
	defer arenaMapTerrainMu.Unlock()
	if arenaMapTerrainCachedKey != key || arenaMapTerrainRows == nil {
		arenaMapTerrainRows = terrain.ToCompactJSONWithFeatures(pads, zones, capturePads)
		arenaMapTerrainCachedKey = key
	}
	return arenaMapTerrainRows
}

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
		// Snapshot terrain, phase-dependent features, and mode under one engine
		// lock so a round transition cannot produce a hybrid response.
		mapSnapshot := engine.GetArenaMapSnapshot()
		terrain := mapSnapshot.Terrain
		if terrain == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"status":  "no_map",
				"message": "No terrain available yet. The next round's map is pre-generated during intermission — check back then.",
			})
			return
		}

		pads := mapSnapshot.TeleportPads
		zones := mapSnapshot.HazardZones
		capturePads := mapSnapshot.CapturePads

		// Also provide detailed metadata for bots that want extra info
		padViews := make([]game.TeleportPadView, 0, len(pads))
		for _, pad := range pads {
			padViews = append(padViews, game.BuildTeleportPadView(pad, mapSnapshot.Tick, true))
		}

		zoneViews := make([]game.HazardZoneView, 0, len(zones))
		for _, zone := range zones {
			zoneViews = append(zoneViews, game.BuildHazardZoneView(zone, true, mapSnapshot.Modifier))
		}

		captureViews := make([]game.CapturePadView, 0, len(capturePads))
		for _, pad := range capturePads {
			captureViews = append(captureViews, game.BuildCapturePadView(pad, mapSnapshot.Tick, true))
		}

		response := map[string]interface{}{
			"status":    "ok",
			"width":     terrain.Width,
			"height":    terrain.Height,
			"cell_size": terrain.CellSize,
			// Shape of this terrain. Non-square shapes are already carved
			// into the terrain rows as '#' walls; this names the outline so
			// bots and dashboards can adapt strategy per shape.
			"map_shape":        string(mapSnapshot.MapShape),
			"features_pending": mapSnapshot.FeaturesPending,
			"terrain":          compactTerrainRows(terrain, mapSnapshot.FeaturesPending, pads, zones, capturePads),
			"teleport_pads":    padViews,
			"capture_pads":     captureViews,
			"hazard_zones":     zoneViews,
			"legend": map[string]string{
				".": "ground (passable)",
				"#": "wall (blocked)",
				"T": "teleport pad",
				"C": "capture pad objective",
				"H": "hazard zone (damage when active)",
			},
		}
		if !mapSnapshot.FeaturesPending {
			response["game_mode"] = string(mapSnapshot.GameMode)
		}
		writeJSON(w, http.StatusOK, response)
	}
}
