package db

import (
	"strings"
	"testing"
)

// TestGetLatestWinnerBountySeedSQLUsesRestartSafeOrdering guards against
// reverting to ORDER BY round_number, an in-memory counter that resets to 0
// on every process restart - the one scenario this query exists to handle
// correctly. persisted_order is a DB sequence and stays monotonic across
// restarts, matching the sibling ListRecentWeaponPerformance query.
func TestGetLatestWinnerBountySeedSQLUsesRestartSafeOrdering(t *testing.T) {
	if !strings.Contains(getLatestWinnerBountySeedSQL, "ORDER BY r.persisted_order DESC") {
		t.Fatalf("query does not order by the restart-safe persisted_order column: %q", getLatestWinnerBountySeedSQL)
	}
	if strings.Contains(getLatestWinnerBountySeedSQL, "round_number") {
		t.Fatalf("query references round_number, which resets to 0 on every restart: %q", getLatestWinnerBountySeedSQL)
	}
}
