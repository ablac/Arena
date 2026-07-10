package db

import "testing"

func TestPostgresAdminOverridesPersistAndReplaceValues(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureAdminOverridesSchema(ctx); err != nil {
		t.Fatalf("EnsureAdminOverridesSchema: %v", err)
	}

	if err := SaveAdminOverrides(ctx, AdminOverrideScopeGameConfig, map[string]interface{}{
		"round_duration": 180.0,
		"friendly_fire":  true,
	}); err != nil {
		t.Fatalf("SaveAdminOverrides game config: %v", err)
	}
	if err := SaveAdminOverrides(ctx, AdminOverrideScopeWeapon, map[string]interface{}{
		"sword": map[string]interface{}{"damage": 23, "grid_range": 1, "cooldown": 0.6, "param": 0},
	}); err != nil {
		t.Fatalf("SaveAdminOverrides weapon: %v", err)
	}

	gameValues, err := LoadAdminOverrides(ctx, AdminOverrideScopeGameConfig)
	if err != nil {
		t.Fatalf("LoadAdminOverrides game config: %v", err)
	}
	if gameValues["round_duration"] != 180.0 || gameValues["friendly_fire"] != true {
		t.Fatalf("loaded game overrides = %#v", gameValues)
	}
	weaponValues, err := LoadAdminOverrides(ctx, AdminOverrideScopeWeapon)
	if err != nil {
		t.Fatalf("LoadAdminOverrides weapon: %v", err)
	}
	sword, ok := weaponValues["sword"].(map[string]interface{})
	if !ok || sword["damage"] != float64(23) || sword["cooldown"] != 0.6 {
		t.Fatalf("loaded sword override = %#v", weaponValues["sword"])
	}

	if err := SaveAdminOverrides(ctx, AdminOverrideScopeGameConfig, map[string]interface{}{
		"round_duration": 240.0,
	}); err != nil {
		t.Fatalf("replace game override: %v", err)
	}
	gameValues, err = LoadAdminOverrides(ctx, AdminOverrideScopeGameConfig)
	if err != nil || gameValues["round_duration"] != 240.0 || gameValues["friendly_fire"] != true {
		t.Fatalf("reloaded game overrides = (%#v, %v)", gameValues, err)
	}

	deleted, err := DeleteAdminOverride(ctx, AdminOverrideScopeGameConfig, "round_duration")
	if err != nil || !deleted {
		t.Fatalf("DeleteAdminOverride = (%v, %v), want (true, nil)", deleted, err)
	}
	gameValues, err = LoadAdminOverrides(ctx, AdminOverrideScopeGameConfig)
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if _, exists := gameValues["round_duration"]; exists {
		t.Fatalf("deleted override still present: %#v", gameValues)
	}
}
