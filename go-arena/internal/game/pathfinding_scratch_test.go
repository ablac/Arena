package game

import (
	"testing"
)

// buildTestNavGrid returns a 10x10 grid with a vertical wall at x=5 that has
// a single gap at y=8, forcing paths to detour.
func buildTestNavGrid() *NavGrid {
	cols, rows := 10, 10
	blocked := make([][]bool, cols)
	for cx := range blocked {
		blocked[cx] = make([]bool, rows)
	}
	for cy := 0; cy < rows; cy++ {
		if cy != 8 {
			blocked[5][cy] = true
		}
	}
	return &NavGrid{CellSize: 20, Width: cols, Height: rows, Blocked: blocked}
}

// TestFindPathScratchReuseStaysDeterministic guards the flat-scratch A*
// rewrite: repeated searches on one grid must reuse the generation-stamped
// buffers without stale state leaking between calls (identical inputs give
// identical output, and interleaved different queries stay correct).
func TestFindPathScratchReuseStaysDeterministic(t *testing.T) {
	grid := buildTestNavGrid()
	start := NewVec2(30, 30)  // cell (1,1)
	goal := NewVec2(170, 30)  // cell (8,1), other side of the wall
	other := NewVec2(170, 90) // cell (8,4)

	first := FindPath(start, goal, grid)
	if len(first) == 0 {
		t.Fatal("expected a path through the wall gap")
	}
	last := first[len(first)-1]
	if last != goal {
		t.Fatalf("path must end at the exact goal, got %v", last)
	}
	// The wall at x=5 has its only gap at y=8, so any valid path must pass
	// through that gap's row.
	sawGap := false
	for _, wp := range first {
		if wp.Y() > 150 {
			sawGap = true
		}
	}
	if !sawGap {
		t.Fatalf("path did not route through the wall gap: %v", first)
	}

	// Interleave a different query, then repeat the first: identical result.
	if p := FindPath(start, other, grid); len(p) == 0 {
		t.Fatal("expected a path for the interleaved query")
	}
	second := FindPath(start, goal, grid)
	if len(second) != len(first) {
		t.Fatalf("scratch reuse changed the path: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("scratch reuse changed waypoint %d: %v vs %v", i, first[i], second[i])
		}
	}
}

// TestFindPathBlockedEndpointsAndNoPath covers the nearestUnblocked scratch
// path and the unreachable-goal exit.
func TestFindPathBlockedEndpointsAndNoPath(t *testing.T) {
	grid := buildTestNavGrid()

	// Goal inside the wall: rerouted to the nearest unblocked cell.
	goalInWall := NewVec2(110, 70) // cell (5,3), blocked
	if p := FindPath(NewVec2(30, 70), goalInWall, grid); len(p) == 0 {
		t.Fatal("expected a path to the nearest unblocked cell")
	}

	// Fully sealed goal region: no path.
	sealed := buildTestNavGrid()
	for cy := 0; cy < sealed.Height; cy++ {
		sealed.Blocked[5][cy] = true // close the gap
	}
	if p := FindPath(NewVec2(30, 30), NewVec2(170, 30), sealed); p != nil {
		t.Fatalf("expected nil path across a sealed wall, got %v", p)
	}
}
