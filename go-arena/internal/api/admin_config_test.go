package api

import (
	"reflect"
	"testing"

	"arena-server/internal/config"
)

func TestApplyGameConfigUpdates_CoupledBoundsAreAppliedAtomically(t *testing.T) {
	original := config.C
	t.Cleanup(func() { config.C = original })

	tests := []struct {
		name    string
		updates map[string]interface{}
		assert  func(*testing.T)
	}{
		{
			name: "obstacle counts can both move above old maximum",
			updates: map[string]interface{}{
				"obstacle_count_min": float64(30),
				"obstacle_count_max": float64(45),
			},
			assert: func(t *testing.T) {
				if config.C.ObstacleCountMin != 30 || config.C.ObstacleCountMax != 45 {
					t.Fatalf("obstacle bounds = %d..%d, want 30..45", config.C.ObstacleCountMin, config.C.ObstacleCountMax)
				}
			},
		},
		{
			name: "arena bot bounds can both move above old maximum",
			updates: map[string]interface{}{
				"arena_size_base_bots": float64(60),
				"arena_size_max_bots":  float64(80),
			},
			assert: func(t *testing.T) {
				if config.C.ArenaSizeBaseBots != 60 || config.C.ArenaSizeMaxBots != 80 {
					t.Fatalf("arena bot bounds = %d..%d, want 60..80", config.C.ArenaSizeBaseBots, config.C.ArenaSizeMaxBots)
				}
			},
		},
		{
			name: "arena scale bounds can both move above old maximum",
			updates: map[string]interface{}{
				"arena_size_min_scale": float64(3),
				"arena_size_max_scale": float64(4),
			},
			assert: func(t *testing.T) {
				if config.C.ArenaSizeMinScale != 3 || config.C.ArenaSizeMaxScale != 4 {
					t.Fatalf("arena scale bounds = %.1f..%.1f, want 3.0..4.0", config.C.ArenaSizeMinScale, config.C.ArenaSizeMaxScale)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.C.ObstacleCountMin, config.C.ObstacleCountMax = 5, 10
			config.C.ArenaSizeBaseBots, config.C.ArenaSizeMaxBots = 12, 48
			config.C.ArenaSizeMinScale, config.C.ArenaSizeMaxScale = 0.6, 2

			applied := applyGameConfigUpdates(tt.updates)
			if len(applied) != 2 {
				t.Fatalf("applied = %#v, want both coupled values", applied)
			}
			tt.assert(t)
		})
	}
}

func TestApplyGameConfigUpdates_InvalidCoupledBoundsRejectBothValues(t *testing.T) {
	original := config.C
	t.Cleanup(func() { config.C = original })
	config.C.ObstacleCountMin, config.C.ObstacleCountMax = 5, 10

	updates := map[string]interface{}{
		"obstacle_count_min": float64(30),
		"obstacle_count_max": float64(20),
	}
	applied := applyGameConfigUpdates(updates)

	if len(applied) != 0 {
		t.Fatalf("applied = %#v, want invalid pair rejected atomically", applied)
	}
	if config.C.ObstacleCountMin != 5 || config.C.ObstacleCountMax != 10 {
		t.Fatalf("obstacle bounds changed to %d..%d after invalid update", config.C.ObstacleCountMin, config.C.ObstacleCountMax)
	}
	if got, want := rejectedConfigKeys(updates, applied), []string{"obstacle_count_max", "obstacle_count_min"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected keys = %#v, want %#v", got, want)
	}
}

func TestApplyGameConfigUpdates_AcceptsEveryExportedScalar(t *testing.T) {
	original := config.C
	t.Cleanup(func() { config.C = original })

	updates := map[string]interface{}{
		"arena_width":      float64(1800),
		"arena_height":     float64(1600),
		"stat_budget":      float64(24),
		"projectile_speed": float64(260),
	}
	applied := applyGameConfigUpdates(updates)

	if got, want := rejectedConfigKeys(updates, applied), []string{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected keys = %#v, want none", got)
	}
	if config.C.ArenaWidth != 1800 || config.C.ArenaHeight != 1600 || config.C.StatBudget != 24 || config.C.ProjectileSpeed != 260 {
		t.Fatalf("exported config values were not all applied: width=%v height=%v budget=%v projectile=%v",
			config.C.ArenaWidth, config.C.ArenaHeight, config.C.StatBudget, config.C.ProjectileSpeed)
	}
}
