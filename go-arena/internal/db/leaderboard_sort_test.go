package db

import "testing"

func TestAllTimeLeaderboardSupportsEveryAdminSortOption(t *testing.T) {
	for _, sort := range []string{"kills", "elo", "kd_ratio", "streak", "wins", "damage"} {
		if _, ok := validSortColumns[sort]; !ok {
			t.Errorf("admin leaderboard sort %q silently falls back instead of using its requested metric", sort)
		}
	}
}
