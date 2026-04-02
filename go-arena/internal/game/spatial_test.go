package game

import (
	"testing"
)

func TestSpatialGridInsertQuery(t *testing.T) {
	g := NewSpatialGrid(100)

	g.Insert("a", NewVec2(50, 50))
	g.Insert("b", NewVec2(150, 50))
	g.Insert("c", NewVec2(50, 150))

	// Query radius covering just "a"
	results := g.QueryRadius(NewVec2(50, 50), 10)
	if len(results) != 1 || results[0] != "a" {
		t.Errorf("expected [a], got %v", results)
	}
}

func TestSpatialGridQueryRadiusMultiple(t *testing.T) {
	g := NewSpatialGrid(100)
	g.Insert("a", NewVec2(500, 500))
	g.Insert("b", NewVec2(510, 500))
	g.Insert("c", NewVec2(600, 500)) // outside radius

	results := g.QueryRadius(NewVec2(500, 500), 50)
	found := make(map[string]bool)
	for _, r := range results {
		found[r] = true
	}
	if !found["a"] || !found["b"] {
		t.Errorf("expected a and b in results, got %v", results)
	}
	if found["c"] {
		t.Errorf("c should not be in results")
	}
}

func TestSpatialGridRemove(t *testing.T) {
	g := NewSpatialGrid(100)
	g.Insert("x", NewVec2(50, 50))
	g.Remove("x")

	results := g.QueryRadius(NewVec2(50, 50), 200)
	for _, r := range results {
		if r == "x" {
			t.Error("removed entity still found")
		}
	}
}

func TestSpatialGridRemoveNonExistent(t *testing.T) {
	g := NewSpatialGrid(100)
	// Should not panic
	g.Remove("doesnotexist")
}

func TestSpatialGridUpdate(t *testing.T) {
	g := NewSpatialGrid(100)
	g.Insert("a", NewVec2(50, 50))
	// Move to a new cell
	g.Update("a", NewVec2(550, 550))

	pos, ok := g.GetPosition("a")
	if !ok {
		t.Fatal("entity not found after update")
	}
	if pos.X() != 550 || pos.Y() != 550 {
		t.Errorf("wrong position after update: %v", pos)
	}

	// Old location should not contain it
	results := g.QueryRadius(NewVec2(50, 50), 10)
	for _, r := range results {
		if r == "a" {
			t.Error("entity still in old cell after move")
		}
	}
}

func TestSpatialGridUpdateNewEntity(t *testing.T) {
	g := NewSpatialGrid(100)
	// Update on non-existent should insert
	g.Update("new", NewVec2(200, 200))
	pos, ok := g.GetPosition("new")
	if !ok {
		t.Fatal("entity not found after Update-insert")
	}
	if pos.X() != 200 {
		t.Errorf("wrong position: %v", pos)
	}
}

func TestSpatialGridGetPositionMissing(t *testing.T) {
	g := NewSpatialGrid(100)
	_, ok := g.GetPosition("missing")
	if ok {
		t.Error("expected ok=false for missing entity")
	}
}

func TestSpatialGridClear(t *testing.T) {
	g := NewSpatialGrid(100)
	g.Insert("a", NewVec2(50, 50))
	g.Insert("b", NewVec2(150, 150))
	g.Clear()

	results := g.QueryRadius(NewVec2(50, 50), 10000)
	if len(results) != 0 {
		t.Errorf("expected empty after clear, got %v", results)
	}
}

func TestSpatialGridInsertDuplicate(t *testing.T) {
	g := NewSpatialGrid(100)
	g.Insert("a", NewVec2(50, 50))
	// Inserting same ID at new location should update position
	g.Insert("a", NewVec2(250, 250))

	pos, ok := g.GetPosition("a")
	if !ok {
		t.Fatal("entity not found")
	}
	if pos.X() != 250 {
		t.Errorf("expected updated position 250, got %v", pos.X())
	}
	// Old cell should not have the entry
	results := g.QueryRadius(NewVec2(50, 50), 10)
	for _, r := range results {
		if r == "a" {
			t.Error("entity still in old cell after duplicate insert")
		}
	}
}

func TestSpatialGridQueryRadiusEmpty(t *testing.T) {
	g := NewSpatialGrid(100)
	results := g.QueryRadius(NewVec2(0, 0), 1000)
	if len(results) != 0 {
		t.Errorf("expected empty results from empty grid")
	}
}
