package db

import (
	"strings"
	"testing"
)

func TestApplyBotStatsDeltaSQLUsesAtomicIncrements(t *testing.T) {
	required := []string{
		"kills = bot_stats.kills + EXCLUDED.kills",
		"deaths = bot_stats.deaths + EXCLUDED.deaths",
		"damage_dealt = bot_stats.damage_dealt + EXCLUDED.damage_dealt",
		"damage_taken = bot_stats.damage_taken + EXCLUDED.damage_taken",
		"rounds_played = bot_stats.rounds_played + EXCLUDED.rounds_played",
		"round_wins = bot_stats.round_wins + EXCLUDED.round_wins",
		"pickups_collected = bot_stats.pickups_collected + EXCLUDED.pickups_collected",
		"distance_traveled = bot_stats.distance_traveled + EXCLUDED.distance_traveled",
		"updated_at = GREATEST(bot_stats.updated_at, EXCLUDED.updated_at)",
	}
	for _, clause := range required {
		if !strings.Contains(applyBotStatsDeltaSQL, clause) {
			t.Errorf("atomic delta SQL missing %q", clause)
		}
	}
}
