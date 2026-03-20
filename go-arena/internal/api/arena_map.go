package api

import (
	"net/http"

	"arena-server/internal/game"
)

// GetArenaMap returns the current terrain grid as JSON.
// This is the REST equivalent of the WebSocket map_init message.
func GetArenaMap(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		terrain := engine.Terrain
		if terrain == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error":   "map not yet generated",
				"message": "The map is generated at round start. Try again shortly.",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
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
		})
	}
}
