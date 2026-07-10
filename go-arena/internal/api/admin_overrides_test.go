package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

func loadAdminOverrideTestConfig(t *testing.T) {
	t.Helper()
	previousConfig := config.C
	previousWeapons := make(map[string]game.WeaponConfig)
	for _, name := range game.GetAvailableWeapons() {
		if wc, ok := game.GetBaseWeaponConfig(name); ok {
			previousWeapons[name] = wc
		}
	}
	config.Load()
	t.Cleanup(func() {
		config.C = previousConfig
		for name, wc := range previousWeapons {
			game.UpdateBaseWeaponConfig(name, wc)
		}
	})
}

func TestAdminConfigWritesFailClosedWithoutPersistence(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	previousPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = previousPool })
	h := &AdminHandler{}

	t.Run("game config", func(t *testing.T) {
		before := config.C.RoundDuration
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/game/config", strings.NewReader(`{"round_duration":180}`))
		rec := httptest.NewRecorder()
		h.updateGameConfig(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
		}
		if config.C.RoundDuration != before {
			t.Fatalf("runtime config changed without persistence: %v -> %v", before, config.C.RoundDuration)
		}
	})

	t.Run("weapon", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/weapons/sword", strings.NewReader(`{"damage":99}`))
		rec := httptest.NewRecorder()
		h.updateWeapon(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestDecodePersistedWeaponUsesGridRange(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	config.C.PathfindingCellSize = 20
	game.InitWeaponRanges(config.C.PathfindingCellSize)
	original, ok := game.GetBaseWeaponConfig("sword")
	if !ok {
		t.Fatal("missing sword config")
	}
	t.Cleanup(func() { game.UpdateBaseWeaponConfig("sword", original) })

	wc, err := decodePersistedWeapon("sword", map[string]interface{}{
		"damage": 23.0, "grid_range": 2.0, "cooldown": 0.7, "param": 0.25,
	})
	if err != nil {
		t.Fatalf("decodePersistedWeapon: %v", err)
	}
	if wc.Damage != 23 || wc.GridRange != 2 || wc.Range != 40 || wc.Cooldown != 0.7 || wc.Param != 0.25 {
		t.Fatalf("decoded weapon = %+v", wc)
	}
	encoded := persistedWeaponValue(wc)
	if encoded["damage"] != 23 || encoded["grid_range"] != 2 || encoded["cooldown"] != 0.7 {
		t.Fatalf("persisted weapon value = %#v", encoded)
	}
}

func TestDecodePersistedWeaponRejectsIncompleteOrUnknownValues(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	game.InitWeaponRanges(config.C.PathfindingCellSize)

	if _, err := decodePersistedWeapon("laser", map[string]interface{}{
		"damage": 1.0, "grid_range": 1.0, "cooldown": 1.0, "param": 0.0,
	}); err == nil {
		t.Fatal("unknown weapon was accepted")
	}
	if _, err := decodePersistedWeapon("sword", map[string]interface{}{
		"damage": 23.0, "grid_range": 1.0, "cooldown": 0.0,
	}); err == nil {
		t.Fatal("invalid persisted weapon was accepted")
	}
}

func TestAdminConfigFailedSaveCannotRollbackConcurrentSuccess(t *testing.T) {
	loadAdminOverrideTestConfig(t)
	config.C.RoundDuration = 120

	firstSaveEntered := make(chan struct{})
	releaseFirstSave := make(chan struct{})
	secondHandlerStarted := make(chan struct{})
	var stateMu sync.Mutex
	saveCalls := 0
	persistedDuration := float64(0)
	h := &AdminHandler{}
	h.saveAdminOverrides = func(_ context.Context, scope string, values map[string]interface{}) error {
		if scope != db.AdminOverrideScopeGameConfig {
			t.Fatalf("scope = %q", scope)
		}
		stateMu.Lock()
		saveCalls++
		call := saveCalls
		stateMu.Unlock()
		if call == 1 {
			close(firstSaveEntered)
			<-releaseFirstSave
			return errors.New("injected first-save failure")
		}
		stateMu.Lock()
		persistedDuration, _ = values["round_duration"].(float64)
		stateMu.Unlock()
		return nil
	}

	type result struct {
		status int
		body   string
	}
	request := func(duration int, started chan<- struct{}) result {
		if started != nil {
			close(started)
		}
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/game/config", strings.NewReader(
			`{"round_duration":`+strconv.Itoa(duration)+`}`,
		))
		rec := httptest.NewRecorder()
		h.updateGameConfig(rec, req)
		return result{status: rec.Code, body: rec.Body.String()}
	}

	firstDone := make(chan result, 1)
	go func() { firstDone <- request(180, nil) }()
	select {
	case <-firstSaveEntered:
	case <-time.After(time.Second):
		t.Fatal("first save did not start")
	}

	secondDone := make(chan result, 1)
	go func() { secondDone <- request(240, secondHandlerStarted) }()
	<-secondHandlerStarted
	select {
	case result := <-secondDone:
		t.Fatalf("second request bypassed override transaction lock: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirstSave)
	first := <-firstDone
	second := <-secondDone
	if first.status != http.StatusInternalServerError {
		t.Fatalf("first status = %d body=%s", first.status, first.body)
	}
	if second.status != http.StatusOK {
		t.Fatalf("second status = %d body=%s", second.status, second.body)
	}
	stateMu.Lock()
	finalPersisted := persistedDuration
	stateMu.Unlock()
	if finalPersisted != 240 || config.C.RoundDuration != 120 {
		t.Fatalf("persisted/active duration = %.0f/%.0f, want 240/120", finalPersisted, config.C.RoundDuration)
	}
	h.overrideMu.Lock()
	desired, _, err := h.desiredGameConfigLocked()
	h.overrideMu.Unlock()
	if err != nil || desired.RoundDuration != 240 {
		t.Fatalf("desired restart duration = %.0f, err=%v; want 240", desired.RoundDuration, err)
	}
}
