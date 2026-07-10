package api

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

// LoadPersistedGameConfigOverrides applies validated Admin Panel values after
// environment loading and before the engine is constructed.
func LoadPersistedGameConfigOverrides(ctx context.Context) error {
	values, err := db.LoadAdminOverrides(ctx, db.AdminOverrideScopeGameConfig)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	candidate, applied := applyGameConfigUpdatesTo(config.C, values)
	rejected := rejectedConfigKeys(values, applied)
	if len(rejected) > 0 {
		return fmt.Errorf("persisted game config contains rejected keys: %v", rejected)
	}
	config.C = candidate
	slog.Info("loaded persisted admin game config", "keys", sortedMapKeys(applied))
	return nil
}

// LoadPersistedWeaponOverrides runs after weapon ranges are initialized and
// before saved adaptive scales are loaded.
func LoadPersistedWeaponOverrides(ctx context.Context) error {
	values, err := db.LoadAdminOverrides(ctx, db.AdminOverrideScopeWeapon)
	if err != nil {
		return err
	}
	decoded := make(map[string]game.WeaponConfig, len(values))
	for name, raw := range values {
		wc, err := decodePersistedWeapon(name, raw)
		if err != nil {
			return err
		}
		decoded[name] = wc
	}
	for name, wc := range decoded {
		if !game.UpdateBaseWeaponConfig(name, wc) {
			return fmt.Errorf("persisted weapon %q is not supported", name)
		}
	}
	if len(values) > 0 {
		slog.Info("loaded persisted admin weapon overrides", "weapons", sortedMapKeys(values))
	}
	return nil
}

func decodePersistedWeapon(name string, raw interface{}) (game.WeaponConfig, error) {
	wc, ok := game.GetBaseWeaponConfig(name)
	if !ok {
		return game.WeaponConfig{}, fmt.Errorf("unknown persisted weapon %q", name)
	}
	values, ok := raw.(map[string]interface{})
	if !ok {
		return game.WeaponConfig{}, fmt.Errorf("persisted weapon %q is not an object", name)
	}
	damage, damageOK := toInt(values["damage"])
	gridRange, rangeOK := toInt(values["grid_range"])
	cooldown, cooldownOK := toFloat(values["cooldown"])
	param, paramOK := toFloat(values["param"])
	if !damageOK || damage < 0 || !rangeOK || gridRange < 0 || !cooldownOK || cooldown <= 0 || !paramOK {
		return game.WeaponConfig{}, fmt.Errorf("persisted weapon %q has invalid values", name)
	}
	wc.Damage = damage
	wc.GridRange = gridRange
	wc.Cooldown = cooldown
	wc.Param = param
	wc.Range = float64(gridRange) * config.C.PathfindingCellSize
	return wc, nil
}

func persistedWeaponValue(wc game.WeaponConfig) map[string]interface{} {
	return map[string]interface{}{
		"damage":     wc.Damage,
		"grid_range": wc.GridRange,
		"cooldown":   wc.Cooldown,
		"param":      wc.Param,
	}
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
