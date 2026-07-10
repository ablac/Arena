package config

import "testing"

func TestResolveEloSettings(t *testing.T) {
	tests := []struct {
		name                           string
		min, max, starting             int
		wantMin, wantMax, wantStarting int
	}{
		{name: "valid custom values", min: 800, max: 1600, starting: 1200, wantMin: 800, wantMax: 1600, wantStarting: 1200},
		{name: "inverted bounds use one default pair", min: 5000, max: 2000, starting: 1500, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1500},
		{name: "nonpositive bounds use one default pair", min: 0, max: 0, starting: 1000, wantMin: DefaultEloMin, wantMax: DefaultEloMax, wantStarting: 1000},
		{name: "high starting rating clamps", min: 800, max: 1200, starting: 5000, wantMin: 800, wantMax: 1200, wantStarting: 1200},
		{name: "low starting rating clamps", min: 800, max: 1200, starting: 200, wantMin: 800, wantMax: 1200, wantStarting: 800},
		{name: "missing starting rating uses bounded default", min: 1500, max: 2000, starting: 0, wantMin: 1500, wantMax: 2000, wantStarting: 1500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minElo, maxElo, startingElo := resolveEloSettings(tt.min, tt.max, tt.starting)
			if minElo != tt.wantMin || maxElo != tt.wantMax || startingElo != tt.wantStarting {
				t.Fatalf("resolveEloSettings(%d, %d, %d) = (%d, %d, %d), want (%d, %d, %d)",
					tt.min, tt.max, tt.starting, minElo, maxElo, startingElo,
					tt.wantMin, tt.wantMax, tt.wantStarting)
			}
		})
	}
}

func TestEloHelpersUseSameDefensiveBounds(t *testing.T) {
	previous := C
	t.Cleanup(func() { C = previous })
	C.EloMin = 5000
	C.EloMax = 2000
	C.EloStarting = 9000

	minElo, maxElo := EloBounds()
	if minElo != DefaultEloMin || maxElo != DefaultEloMax {
		t.Fatalf("EloBounds() = %d..%d, want %d..%d", minElo, maxElo, DefaultEloMin, DefaultEloMax)
	}
	if got := StartingElo(); got != DefaultEloMax {
		t.Fatalf("StartingElo() = %d, want %d", got, DefaultEloMax)
	}
	if got := ClampElo(-1); got != DefaultEloMin {
		t.Fatalf("ClampElo(-1) = %d, want %d", got, DefaultEloMin)
	}
	if got := ClampElo(99999); got != DefaultEloMax {
		t.Fatalf("ClampElo(99999) = %d, want %d", got, DefaultEloMax)
	}
}
