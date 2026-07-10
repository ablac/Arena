package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	AdminOverrideScopeGameConfig = "game_config"
	AdminOverrideScopeWeapon     = "weapon"
)

func validAdminOverrideScope(scope string) bool {
	return scope == AdminOverrideScopeGameConfig || scope == AdminOverrideScopeWeapon
}

// EnsureAdminOverridesSchema creates the durable store behind Admin Panel
// controls that are expected to survive a restart or self-update.
func EnsureAdminOverridesSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS admin_runtime_overrides (
			scope TEXT NOT NULL CHECK (scope IN ('game_config', 'weapon')),
			key TEXT NOT NULL,
			value JSONB NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (scope, key)
		)`)
	if err != nil {
		return fmt.Errorf("EnsureAdminOverridesSchema: %w", err)
	}
	return nil
}

// SaveAdminOverrides atomically upserts every successfully validated value.
// An empty set is a no-op.
func SaveAdminOverrides(ctx context.Context, scope string, values map[string]interface{}) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	if !validAdminOverrideScope(scope) {
		return fmt.Errorf("invalid admin override scope %q", scope)
	}
	if len(values) == 0 {
		return nil
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("SaveAdminOverrides begin: %w", err)
	}
	defer tx.Rollback(ctx)
	for key, value := range values {
		if key == "" {
			return errors.New("admin override key is required")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode admin override %s/%s: %w", scope, key, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_runtime_overrides (scope, key, value, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (scope, key) DO UPDATE
			SET value = EXCLUDED.value, updated_at = NOW()`, scope, key, encoded); err != nil {
			return fmt.Errorf("save admin override %s/%s: %w", scope, key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("SaveAdminOverrides commit: %w", err)
	}
	return nil
}

// LoadAdminOverrides returns decoded JSON values for one allowlisted scope.
func LoadAdminOverrides(ctx context.Context, scope string) (map[string]interface{}, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if !validAdminOverrideScope(scope) {
		return nil, fmt.Errorf("invalid admin override scope %q", scope)
	}
	rows, err := Pool.Query(ctx, `
		SELECT key, value
		FROM admin_runtime_overrides
		WHERE scope = $1
		ORDER BY key`, scope)
	if err != nil {
		return nil, fmt.Errorf("LoadAdminOverrides: %w", err)
	}
	defer rows.Close()
	values := make(map[string]interface{})
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, fmt.Errorf("LoadAdminOverrides scan: %w", err)
		}
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode admin override %s/%s: %w", scope, key, err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("LoadAdminOverrides rows: %w", err)
	}
	return values, nil
}

// DeleteAdminOverride removes one durable override so a future restart can
// fall back to environment/default configuration.
func DeleteAdminOverride(ctx context.Context, scope, key string) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	if !validAdminOverrideScope(scope) {
		return false, fmt.Errorf("invalid admin override scope %q", scope)
	}
	tag, err := Pool.Exec(ctx, `
		DELETE FROM admin_runtime_overrides WHERE scope = $1 AND key = $2`, scope, key)
	if err != nil {
		return false, fmt.Errorf("DeleteAdminOverride: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
