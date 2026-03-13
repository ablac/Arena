"""Spatial grid for efficient proximity queries in the arena."""

import math
from collections import defaultdict

from server.config import settings


class SpatialGrid:
    """Grid-based spatial index for O(1) cell lookups and fast radius queries.

    Divides the arena into cells. Each entity maps to one cell based on position.
    Radius queries only check neighboring cells that could contain matches.
    """

    def __init__(
        self,
        width: int | None = None,
        height: int | None = None,
        cell_size: int | None = None,
    ) -> None:
        """Initialize spatial grid.

        Args:
            width: Arena width in units. Defaults to config value.
            height: Arena height in units. Defaults to config value.
            cell_size: Grid cell size in units. Defaults to config value.
        """
        self.width = width or settings.game.arena_width
        self.height = height or settings.game.arena_height
        self.cell_size = cell_size or settings.game.spatial_cell_size
        self.cols = math.ceil(self.width / self.cell_size)
        self.rows = math.ceil(self.height / self.cell_size)

        # cell (cx, cy) -> set of entity_ids
        self._cells: dict[tuple[int, int], set[str]] = defaultdict(set)
        # entity_id -> (cx, cy) current cell
        self._entity_cells: dict[str, tuple[int, int]] = {}
        # entity_id -> (x, y) exact position
        self._positions: dict[str, tuple[float, float]] = {}

    def _get_cell(self, x: float, y: float) -> tuple[int, int]:
        """Get grid cell coordinates for a world position."""
        cx = min(int(x / self.cell_size), self.cols - 1)
        cy = min(int(y / self.cell_size), self.rows - 1)
        return (max(0, cx), max(0, cy))

    def insert(self, entity_id: str, x: float, y: float) -> None:
        """Add an entity to the grid at the given position.

        If the entity already exists it is removed first to prevent ghost entries.
        """
        if entity_id in self._entity_cells:
            self.remove(entity_id)
        cell = self._get_cell(x, y)
        self._cells[cell].add(entity_id)
        self._entity_cells[entity_id] = cell
        self._positions[entity_id] = (x, y)

    def remove(self, entity_id: str) -> None:
        """Remove an entity from the grid."""
        cell = self._entity_cells.pop(entity_id, None)
        if cell is not None:
            self._cells[cell].discard(entity_id)
        self._positions.pop(entity_id, None)

    def update(self, entity_id: str, x: float, y: float) -> None:
        """Move an entity to a new position, updating its cell if needed."""
        new_cell = self._get_cell(x, y)
        old_cell = self._entity_cells.get(entity_id)

        if old_cell != new_cell:
            if old_cell is not None:
                self._cells[old_cell].discard(entity_id)
            self._cells[new_cell].add(entity_id)
            self._entity_cells[entity_id] = new_cell

        self._positions[entity_id] = (x, y)

    def query_radius(self, x: float, y: float, radius: float) -> list[str]:
        """Find all entity IDs within radius of (x, y).

        Only checks cells that could contain entities within the radius.
        """
        radius_sq = radius * radius
        # Determine cell range to check
        min_cx = max(0, int((x - radius) / self.cell_size))
        max_cx = min(self.cols - 1, int((x + radius) / self.cell_size))
        min_cy = max(0, int((y - radius) / self.cell_size))
        max_cy = min(self.rows - 1, int((y + radius) / self.cell_size))

        result: list[str] = []
        for cx in range(min_cx, max_cx + 1):
            for cy in range(min_cy, max_cy + 1):
                for eid in self._cells.get((cx, cy), set()):
                    pos = self._positions[eid]
                    dx = pos[0] - x
                    dy = pos[1] - y
                    if dx * dx + dy * dy <= radius_sq:
                        result.append(eid)
        return result

    def query_cell(self, cx: int, cy: int) -> list[str]:
        """Get all entity IDs in a specific grid cell."""
        return list(self._cells.get((cx, cy), set()))

    def get_position(self, entity_id: str) -> tuple[float, float] | None:
        """Get the stored position of an entity."""
        return self._positions.get(entity_id)

    def clear(self) -> None:
        """Remove all entities from the grid."""
        self._cells.clear()
        self._entity_cells.clear()
        self._positions.clear()
