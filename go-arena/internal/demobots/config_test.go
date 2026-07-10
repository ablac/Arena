package demobots

import (
	"fmt"
	"testing"
)

func TestDemoConfigsHaveValidDiverseWeaponCohorts(t *testing.T) {
	const statBudget = 20
	requiredStats := []string{"hp", "speed", "attack", "defense"}
	byWeapon := make(map[string][]BotConfig)

	if len(DemoConfigs) != 14 {
		t.Fatalf("demo config count = %d, want 14", len(DemoConfigs))
	}
	for _, cfg := range DemoConfigs {
		t.Run(cfg.Name, func(t *testing.T) {
			total := 0
			if len(cfg.Stats) != len(requiredStats) {
				t.Fatalf("stats = %v, want exactly %v", cfg.Stats, requiredStats)
			}
			for _, stat := range requiredStats {
				value, ok := cfg.Stats[stat]
				if !ok {
					t.Fatalf("missing %q stat", stat)
				}
				if value < 1 || value > 10 {
					t.Fatalf("%s = %d, want 1..10", stat, value)
				}
				total += value
			}
			if total != statBudget {
				t.Fatalf("stat total = %d, want %d", total, statBudget)
			}
			if got := configuredStrategy(cfg.Weapon, cfg.Strategy); got != cfg.Strategy {
				t.Fatalf("strategy %q is not valid for %s (normalized to %q)", cfg.Strategy, cfg.Weapon, got)
			}
		})
		byWeapon[cfg.Weapon] = append(byWeapon[cfg.Weapon], cfg)
	}

	for weapon, cohort := range byWeapon {
		if len(cohort) != 2 {
			t.Errorf("%s cohort has %d templates, want 2", weapon, len(cohort))
			continue
		}
		fingerprint := func(cfg BotConfig) string {
			return fmt.Sprintf("%d/%d/%d/%d:%s",
				cfg.Stats["hp"], cfg.Stats["speed"], cfg.Stats["attack"], cfg.Stats["defense"], cfg.Strategy)
		}
		if fingerprint(cohort[0]) == fingerprint(cohort[1]) {
			t.Errorf("%s templates are an identical cohort: %s", weapon, fingerprint(cohort[0]))
		}
	}
}

func TestNewDemoBotPreservesDeclaredStrategy(t *testing.T) {
	for _, cfg := range DemoConfigs {
		bot := newDemoBot(cfg, "http://127.0.0.1:1")
		if bot.strategy != cfg.Strategy {
			t.Errorf("%s strategy = %q, want declared %q", cfg.Name, bot.strategy, cfg.Strategy)
		}
		bot.applyConfiguredStrategy("round_start")
		if bot.strategy != cfg.Strategy {
			t.Errorf("%s strategy after round start = %q, want stable %q", cfg.Name, bot.strategy, cfg.Strategy)
		}
	}
}
