package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

func TestMapShapePoolCanonicalPersistenceRoundTrip(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	base := config.C
	base.MapShapePool = "square"

	_, saved := applyGameConfigUpdatesTo(base, map[string]interface{}{
		"map_shape_pool": "circle,hexagon",
	})
	if got, ok := saved["map_shape_pool"].(string); !ok || got != "circle,hexagon" {
		t.Fatalf("canonical saved pool = %#v, want string circle,hexagon", saved["map_shape_pool"])
	}

	// Exercise the same JSON shape produced by PostgreSQL JSONB decoding.
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded map[string]interface{}
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatal(err)
	}
	candidate, applied := applyGameConfigUpdatesTo(base, reloaded)
	if rejected := rejectedConfigKeys(reloaded, applied); len(rejected) != 0 {
		t.Fatalf("round-trip rejected keys = %v", rejected)
	}
	if candidate.MapShapePool != "circle,hexagon" {
		t.Fatalf("round-trip pool = %q", candidate.MapShapePool)
	}

	// Rows written by the broken release used an array. Keep them recoverable
	// while all new writes use the canonical scalar representation.
	legacy, legacyApplied := applyGameConfigUpdatesTo(base, map[string]interface{}{
		"map_shape_pool": []interface{}{"diamond", "cross"},
	})
	if legacy.MapShapePool != "diamond,cross" || legacyApplied["map_shape_pool"] != "diamond,cross" {
		t.Fatalf("legacy pool was not canonicalized: config=%q applied=%#v", legacy.MapShapePool, legacyApplied)
	}
}

func TestCustomMapOverrideSurvivesStartupBeforeRegistryLoad(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	base := config.C
	base.MapShape = "square"
	base.MapShapePool = "square"
	const customShape = "custom:restart-map"
	game.RemoveCustomMap("restart-map")
	t.Cleanup(func() {
		game.RemoveCustomMap("restart-map")
		game.SetRandomShapePool(strings.Split(base.MapShapePool, ","))
	})

	// Startup applies game overrides before the custom-map rows are registered.
	// Syntactic validation must preserve the reference through that phase.
	candidate, applied := applyGameConfigUpdatesTo(base, map[string]interface{}{
		"map_shape":      customShape,
		"map_shape_pool": customShape + ",circle",
	})
	if candidate.MapShape != customShape || candidate.MapShapePool != customShape+",circle" {
		t.Fatalf("pre-registry startup dropped custom map: shape=%q pool=%q applied=%#v",
			candidate.MapShape, candidate.MapShapePool, applied)
	}

	config.C = candidate
	registerCustomMapsAndApplyPool([]game.CustomMapTemplate{{
		Name: "restart-map", DisplayName: "Restart Map", BaseShape: "caves", Seed: 7, Enabled: true,
	}}, candidate.MapShapePool)
	got := game.RandomShapePoolNames()
	if len(got) != 2 || got[0] != customShape || got[1] != "circle" {
		t.Fatalf("post-registry pool = %#v, want [%s circle]", got, customShape)
	}
}

func TestMapWorkshopStagesDurablyWithoutChangingActiveConfig(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	config.C.MapShape = "square"
	config.C.MapShapePool = "square,circle"
	config.C.ObstacleCountMin = 4
	config.C.ObstacleCountMax = 10
	active := config.C

	var saved map[string]interface{}
	h := &AdminHandler{
		activeConfig:    active,
		activeConfigSet: true,
		gameOverrides:   make(map[string]interface{}),
		saveAdminOverrides: func(_ context.Context, scope string, values map[string]interface{}) error {
			if scope != db.AdminOverrideScopeGameConfig {
				t.Fatalf("scope = %q", scope)
			}
			saved = cloneOverrideValues(values)
			return nil
		},
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/game/maps", strings.NewReader(`{
		"enabled_shapes":["circle","hexagon"],
		"map_shape":"random",
		"obstacle_count_min":6,
		"obstacle_count_max":12,
		"arena_size_dynamic":true
	}`))
	rec := httptest.NewRecorder()
	h.updateMapSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if saved["map_shape_pool"] != "circle,hexagon" {
		t.Fatalf("saved map pool = %#v", saved["map_shape_pool"])
	}
	if config.C.MapShape != active.MapShape || config.C.MapShapePool != active.MapShapePool ||
		config.C.ObstacleCountMin != active.ObstacleCountMin || config.C.ObstacleCountMax != active.ObstacleCountMax {
		t.Fatalf("active config changed during staged save: before=%+v after=%+v", active, config.C)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["persisted"] != true || body["restart_required"] != true || body["activation"] != "server_restart" {
		t.Fatalf("truthful staging metadata missing: %#v", body)
	}

	getRec := httptest.NewRecorder()
	h.getMapSettings(getRec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/game/maps", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	shapes, _ := got["enabled_shapes"].([]interface{})
	if len(shapes) != 2 || shapes[0] != "circle" || shapes[1] != "hexagon" {
		t.Fatalf("desired map pool not exposed: %#v", got["enabled_shapes"])
	}
	persistence, _ := got["_persistence"].(map[string]interface{})
	if persistence["restart_required"] != true {
		t.Fatalf("pending restart not exposed: %#v", persistence)
	}
}

func TestMapWorkshopPersistenceFailureLeavesActiveAndPendingStateUntouched(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	active := config.C
	h := &AdminHandler{
		activeConfig:    active,
		activeConfigSet: true,
		gameOverrides:   map[string]interface{}{"map_shape": active.MapShape},
		saveAdminOverrides: func(context.Context, string, map[string]interface{}) error {
			return errors.New("database unavailable")
		},
	}
	beforePending := cloneOverrideValues(h.gameOverrides)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/game/maps", strings.NewReader(`{"map_shape":"circle"}`))
	rec := httptest.NewRecorder()
	h.updateMapSettings(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if config.C != active {
		t.Fatal("active config changed after failed map persistence")
	}
	if h.gameOverrides["map_shape"] != beforePending["map_shape"] {
		t.Fatalf("pending state changed after failed save: %#v", h.gameOverrides)
	}
}

func TestAdminConfigStagingDoesNotRaceConcurrentEngineReads(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	active := config.C
	engine := game.NewGameEngine()
	h := &AdminHandler{
		Engine:          engine,
		activeConfig:    active,
		activeConfigSet: true,
		gameOverrides:   make(map[string]interface{}),
		saveAdminOverrides: func(context.Context, string, map[string]interface{}) error {
			return nil
		},
	}

	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for i := 0; i < 500; i++ {
			_ = engine.GetFullGameState() // reads active config.C arena dimensions
		}
	}()
	for i := 0; i < 100; i++ {
		duration := 120 + i
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/game/config", strings.NewReader(
			fmt.Sprintf(`{"round_duration":%d}`, duration),
		))
		rec := httptest.NewRecorder()
		h.updateGameConfig(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	readers.Wait()
	if config.C != active {
		t.Fatal("staged admin writes mutated the active global config")
	}
}
