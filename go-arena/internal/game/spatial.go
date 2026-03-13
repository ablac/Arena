package game

import "math"

// SpatialGrid provides O(1) proximity queries using a fixed-size grid.
type SpatialGrid struct {
	cellSize  float64
	cells     map[[2]int]map[string]bool // grid cell -> set of entity IDs
	positions map[string]Vec2            // entity_id -> position
}

// NewSpatialGrid creates a new spatial grid with the given cell size.
func NewSpatialGrid(cellSize float64) *SpatialGrid {
	return &SpatialGrid{
		cellSize:  cellSize,
		cells:     make(map[[2]int]map[string]bool),
		positions: make(map[string]Vec2),
	}
}

// cellKey returns the grid cell coordinates for a world position.
func (g *SpatialGrid) cellKey(x, y float64) [2]int {
	return [2]int{
		int(math.Floor(x / g.cellSize)),
		int(math.Floor(y / g.cellSize)),
	}
}

// Insert adds an entity to the grid at the given position.
func (g *SpatialGrid) Insert(id string, pos Vec2) {
	key := g.cellKey(pos.X(), pos.Y())
	if g.cells[key] == nil {
		g.cells[key] = make(map[string]bool)
	}
	g.cells[key][id] = true
	g.positions[id] = pos
}

// Remove removes an entity from the grid.
func (g *SpatialGrid) Remove(id string) {
	pos, ok := g.positions[id]
	if !ok {
		return
	}
	key := g.cellKey(pos.X(), pos.Y())
	if cell := g.cells[key]; cell != nil {
		delete(cell, id)
		if len(cell) == 0 {
			delete(g.cells, key)
		}
	}
	delete(g.positions, id)
}

// Update moves an entity to a new position in the grid. Only reassigns cells
// if the entity has moved to a different cell.
func (g *SpatialGrid) Update(id string, pos Vec2) {
	oldPos, ok := g.positions[id]
	if !ok {
		g.Insert(id, pos)
		return
	}

	oldKey := g.cellKey(oldPos.X(), oldPos.Y())
	newKey := g.cellKey(pos.X(), pos.Y())

	if oldKey != newKey {
		// Remove from old cell
		if cell := g.cells[oldKey]; cell != nil {
			delete(cell, id)
			if len(cell) == 0 {
				delete(g.cells, oldKey)
			}
		}
		// Insert into new cell
		if g.cells[newKey] == nil {
			g.cells[newKey] = make(map[string]bool)
		}
		g.cells[newKey][id] = true
	}

	g.positions[id] = pos
}

// QueryRadius returns all entity IDs within the given Euclidean distance of pos.
func (g *SpatialGrid) QueryRadius(pos Vec2, radius float64) []string {
	// Determine the range of cells that could overlap the radius bounding box.
	minCX := int(math.Floor((pos.X() - radius) / g.cellSize))
	maxCX := int(math.Floor((pos.X() + radius) / g.cellSize))
	minCY := int(math.Floor((pos.Y() - radius) / g.cellSize))
	maxCY := int(math.Floor((pos.Y() + radius) / g.cellSize))

	r2 := radius * radius
	var result []string

	for cx := minCX; cx <= maxCX; cx++ {
		for cy := minCY; cy <= maxCY; cy++ {
			cell := g.cells[[2]int{cx, cy}]
			if cell == nil {
				continue
			}
			for id := range cell {
				ePos := g.positions[id]
				dx := ePos.X() - pos.X()
				dy := ePos.Y() - pos.Y()
				if dx*dx+dy*dy <= r2 {
					result = append(result, id)
				}
			}
		}
	}
	return result
}

// GetPosition returns the stored position of an entity and whether it exists.
func (g *SpatialGrid) GetPosition(id string) (Vec2, bool) {
	pos, ok := g.positions[id]
	return pos, ok
}

// Clear removes all entities from the grid.
func (g *SpatialGrid) Clear() {
	g.cells = make(map[[2]int]map[string]bool)
	g.positions = make(map[string]Vec2)
}
