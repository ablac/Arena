package api

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestBuildMapPreviewReturnsTerrainAndPlayablePercent(t *testing.T) {
	prev, err := buildMapPreview(mapPreviewRequest{Shape: "circle", Cols: 48, Rows: 32})
	if err != nil {
		t.Fatalf("buildMapPreview returned error: %v", err)
	}
	if prev.Shape != "circle" || len(prev.Terrain) != 32 {
		t.Fatalf("unexpected preview metadata: %+v", prev)
	}
	if len(prev.Terrain[0]) != 48 {
		t.Fatalf("unexpected preview row width: %d", len(prev.Terrain[0]))
	}
	if prev.PlayablePct <= 0 || prev.PlayablePct > 100 {
		t.Fatalf("playable percentage out of range: %.2f", prev.PlayablePct)
	}
}

func TestIsMissingAdminRegistryTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "undefined table", err: &pgconn.PgError{Code: "42P01"}, want: true},
		{name: "permission denied", err: &pgconn.PgError{Code: "42501"}, want: true},
		{name: "other postgres error", err: &pgconn.PgError{Code: "23505"}, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingAdminRegistryTable(tc.err); got != tc.want {
				t.Fatalf("isMissingAdminRegistryTable() = %v, want %v", got, tc.want)
			}
		})
	}
}
