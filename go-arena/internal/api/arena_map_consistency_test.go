package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/game"
)

func TestArenaMapPendingTerrainOmitsPreviousRoundModeAndFeatures(t *testing.T) {
	oldTerrain := game.ActiveTerrain
	oldShape := game.ActiveMapShape
	oldRules := game.ActiveModeRules
	t.Cleanup(func() {
		game.ActiveTerrain = oldTerrain
		game.ActiveMapShape = oldShape
		game.ActiveModeRules = oldRules
	})

	for _, phase := range []game.RoundPhase{game.PhaseIntermission, game.PhaseLobby} {
		t.Run(phaseName(phase), func(t *testing.T) {
			engine := game.NewGameEngine()
			terrain := game.NewTerrainGrid(200, 200, nil, 20, 0)
			engine.NextTerrain = terrain
			engine.NextMapShape = game.ShapeCircle
			engine.Round.Phase = phase
			engine.TeleportPads = []game.TeleportPad{{ID: "old-teleport", Position: game.NewVec2(50, 50)}}
			engine.HazardZones = []game.HazardZone{{ID: "old-hazard", Position: game.NewVec2(70, 70), Width: 1, Height: 1}}
			engine.CapturePads = []game.CapturePad{{ID: "old-capture", Position: game.NewVec2(90, 90)}}
			game.ActiveTerrain = terrain
			game.ActiveMapShape = game.ShapeCircle
			game.ActiveModeRules = game.ModeRulesFor(game.ModeTeamBattle)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/arena/map", nil)
			GetArenaMap(engine).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}

			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if _, ok := body["game_mode"]; ok {
				t.Fatalf("pending next map leaked previous game mode: %+v", body["game_mode"])
			}
			if pending, _ := body["features_pending"].(bool); !pending {
				t.Fatalf("pending next map did not declare pending features: %+v", body)
			}
			for _, field := range []string{"teleport_pads", "capture_pads", "hazard_zones"} {
				values, _ := body[field].([]interface{})
				if len(values) != 0 {
					t.Fatalf("pending next map leaked previous %s: %+v", field, values)
				}
			}
			rows, _ := body["terrain"].([]interface{})
			for _, row := range rows {
				if strings.ContainsAny(row.(string), "TCH") {
					t.Fatalf("pending terrain contains previous-round feature overlay: %q", row)
				}
			}
		})
	}
}

func TestArenaMapActiveRoundIncludesCoherentModeAndFeatures(t *testing.T) {
	oldTerrain := game.ActiveTerrain
	oldShape := game.ActiveMapShape
	oldRules := game.ActiveModeRules
	t.Cleanup(func() {
		game.ActiveTerrain = oldTerrain
		game.ActiveMapShape = oldShape
		game.ActiveModeRules = oldRules
	})

	engine := game.NewGameEngine()
	terrain := game.NewTerrainGrid(200, 200, nil, 20, 0)
	engine.Round.Phase = game.PhaseActive
	engine.TeleportPads = []game.TeleportPad{{ID: "teleport", Position: game.NewVec2(50, 50)}}
	engine.HazardZones = []game.HazardZone{{ID: "hazard", Position: game.NewVec2(70, 70), Width: 1, Height: 1}}
	engine.CapturePads = []game.CapturePad{{ID: "capture", Position: game.NewVec2(90, 90)}}
	game.ActiveTerrain = terrain
	game.ActiveMapShape = game.ShapeHexagon
	game.ActiveModeRules = game.ModeRulesFor(game.ModeTeamBattle)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/arena/map", nil)
	GetArenaMap(engine).ServeHTTP(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["game_mode"]; got != string(game.ModeTeamBattle) {
		t.Fatalf("active game_mode = %v, want %q", got, game.ModeTeamBattle)
	}
	if pending, _ := body["features_pending"].(bool); pending {
		t.Fatalf("active round reported pending features: %+v", body)
	}
	for _, field := range []string{"teleport_pads", "capture_pads", "hazard_zones"} {
		values, _ := body[field].([]interface{})
		if len(values) != 1 {
			t.Fatalf("active %s length = %d, want 1", field, len(values))
		}
	}
}

func phaseName(phase game.RoundPhase) string {
	if phase == game.PhaseIntermission {
		return "intermission"
	}
	return "lobby_before_round_start"
}
