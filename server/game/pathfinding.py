"""Grid-based A* pathfinding for bot navigation around obstacles."""

from __future__ import annotations

import heapq
import math
from typing import TYPE_CHECKING

from server.config import settings

if TYPE_CHECKING:
    from server.game.state import Obstacle

# Configurable grid cell size for the navigation grid
NAV_CELL_SIZE: int = 20

# 8-directional movement: (dx, dy, cost)
_DIRECTIONS = [
    (1, 0, 1.0),
    (-1, 0, 1.0),
    (0, 1, 1.0),
    (0, -1, 1.0),
    (1, 1, 1.4142),
    (1, -1, 1.4142),
    (-1, 1, 1.4142),
    (-1, -1, 1.4142),
]

# Bot radius padding added to obstacles when building the blocked grid
_BOT_PADDING: float = 2.0


class NavGrid:
    """Navigation grid that marks cells as blocked based on obstacles.

    The grid is rebuilt once per round when obstacles change.
    """

    def __init__(
        self,
        arena_width: int | None = None,
        arena_height: int | None = None,
        cell_size: int | None = None,
    ) -> None:
        self.arena_width = arena_width or settings.game.arena_width
        self.arena_height = arena_height or settings.game.arena_height
        self.cell_size = cell_size or settings.game.pathfinding_cell_size
        self.cols = math.ceil(self.arena_width / self.cell_size)
        self.rows = math.ceil(self.arena_height / self.cell_size)
        # blocked is a flat set of (cx, cy) tuples for O(1) lookup
        self._blocked: set[tuple[int, int]] = set()

    def build(self, obstacles: list[Obstacle]) -> None:
        """Rebuild the blocked-cell set from the current obstacle list."""
        self._blocked.clear()
        pad = _BOT_PADDING
        for obs in obstacles:
            # Expand obstacle by padding
            ox = obs.x - pad
            oy = obs.y - pad
            ow = obs.width + 2 * pad
            oh = obs.height + 2 * pad

            min_cx = max(0, int(ox / self.cell_size))
            min_cy = max(0, int(oy / self.cell_size))
            max_cx = min(self.cols - 1, int((ox + ow) / self.cell_size))
            max_cy = min(self.rows - 1, int((oy + oh) / self.cell_size))

            for cx in range(min_cx, max_cx + 1):
                for cy in range(min_cy, max_cy + 1):
                    self._blocked.add((cx, cy))

    def is_blocked(self, cx: int, cy: int) -> bool:
        """Check if a grid cell is blocked."""
        return (cx, cy) in self._blocked

    def world_to_cell(self, x: float, y: float) -> tuple[int, int]:
        """Convert world coordinates to grid cell indices."""
        cx = min(max(0, int(x / self.cell_size)), self.cols - 1)
        cy = min(max(0, int(y / self.cell_size)), self.rows - 1)
        return (cx, cy)

    def cell_to_world(self, cx: int, cy: int) -> tuple[float, float]:
        """Convert grid cell to world coordinates (cell center)."""
        return (
            (cx + 0.5) * self.cell_size,
            (cy + 0.5) * self.cell_size,
        )


def find_path(
    start: tuple[float, float],
    goal: tuple[float, float],
    nav_grid: NavGrid,
) -> list[tuple[float, float]]:
    """Compute A* path from start to goal, returning a list of world-space waypoints.

    Returns an empty list if no path is found or start/goal are the same cell.
    The start position is NOT included; the goal position IS included as the last waypoint.
    """
    start_cell = nav_grid.world_to_cell(start[0], start[1])
    goal_cell = nav_grid.world_to_cell(goal[0], goal[1])

    if start_cell == goal_cell:
        # Already in the goal cell — just return the exact goal
        return [goal]

    # If goal cell is blocked, find the nearest unblocked cell
    if nav_grid.is_blocked(*goal_cell):
        goal_cell = _nearest_unblocked(goal_cell, nav_grid)
        if goal_cell is None:
            return []

    # If start cell is blocked, find the nearest unblocked cell
    if nav_grid.is_blocked(*start_cell):
        start_cell = _nearest_unblocked(start_cell, nav_grid)
        if start_cell is None:
            return []

    # A* search
    open_set: list[tuple[float, int, tuple[int, int]]] = []
    counter = 0
    g_score: dict[tuple[int, int], float] = {start_cell: 0.0}
    came_from: dict[tuple[int, int], tuple[int, int]] = {}

    h = _heuristic(start_cell, goal_cell)
    heapq.heappush(open_set, (h, counter, start_cell))

    cols = nav_grid.cols
    rows = nav_grid.rows
    blocked = nav_grid._blocked

    while open_set:
        f, _, current = heapq.heappop(open_set)

        if current == goal_cell:
            # Reconstruct path
            path_cells = _reconstruct(came_from, current)
            # Convert to world coords, use exact goal for last point
            waypoints = [nav_grid.cell_to_world(c[0], c[1]) for c in path_cells[:-1]]
            waypoints.append(goal)
            # Smooth path
            waypoints = _smooth_path(waypoints, nav_grid)
            return waypoints

        current_g = g_score[current]

        for ddx, ddy, cost in _DIRECTIONS:
            nx, ny = current[0] + ddx, current[1] + ddy
            if nx < 0 or nx >= cols or ny < 0 or ny >= rows:
                continue
            neighbor = (nx, ny)
            if neighbor in blocked:
                continue

            # For diagonal moves, check that both cardinal neighbors are clear
            if ddx != 0 and ddy != 0:
                if (current[0] + ddx, current[1]) in blocked or \
                   (current[0], current[1] + ddy) in blocked:
                    continue

            tentative_g = current_g + cost
            if tentative_g < g_score.get(neighbor, float("inf")):
                g_score[neighbor] = tentative_g
                came_from[neighbor] = current
                h = _heuristic(neighbor, goal_cell)
                counter += 1
                heapq.heappush(open_set, (tentative_g + h, counter, neighbor))

    # No path found
    return []


def _heuristic(a: tuple[int, int], b: tuple[int, int]) -> float:
    """Octile distance heuristic for 8-directional movement."""
    dx = abs(a[0] - b[0])
    dy = abs(a[1] - b[1])
    return max(dx, dy) + 0.4142 * min(dx, dy)


def _reconstruct(
    came_from: dict[tuple[int, int], tuple[int, int]],
    current: tuple[int, int],
) -> list[tuple[int, int]]:
    """Reconstruct path from came_from map (excludes start cell)."""
    path = [current]
    while current in came_from:
        current = came_from[current]
        path.append(current)
    path.reverse()
    # Remove the start cell
    return path[1:]


def _nearest_unblocked(
    cell: tuple[int, int], nav_grid: NavGrid
) -> tuple[int, int] | None:
    """Find the nearest unblocked cell via BFS. Returns None if none found within range."""
    from collections import deque

    visited: set[tuple[int, int]] = {cell}
    queue: deque[tuple[int, int]] = deque([cell])
    max_search = 200  # limit search to avoid excessive computation

    while queue and max_search > 0:
        max_search -= 1
        cx, cy = queue.popleft()
        for ddx, ddy, _ in _DIRECTIONS:
            nx, ny = cx + ddx, cy + ddy
            if nx < 0 or nx >= nav_grid.cols or ny < 0 or ny >= nav_grid.rows:
                continue
            if (nx, ny) in visited:
                continue
            visited.add((nx, ny))
            if not nav_grid.is_blocked(nx, ny):
                return (nx, ny)
            queue.append((nx, ny))

    return None


def _smooth_path(
    waypoints: list[tuple[float, float]], nav_grid: NavGrid
) -> list[tuple[float, float]]:
    """Remove unnecessary intermediate waypoints using line-of-sight checks.

    Iteratively skip waypoints that can be reached directly without crossing
    blocked cells.
    """
    if len(waypoints) <= 2:
        return waypoints

    smoothed: list[tuple[float, float]] = [waypoints[0]]
    i = 0
    while i < len(waypoints) - 1:
        # Try to skip as far ahead as possible
        farthest = i + 1
        for j in range(len(waypoints) - 1, i + 1, -1):
            if _line_clear(smoothed[-1], waypoints[j], nav_grid):
                farthest = j
                break
        smoothed.append(waypoints[farthest])
        i = farthest

    return smoothed


def _line_clear(
    a: tuple[float, float], b: tuple[float, float], nav_grid: NavGrid
) -> bool:
    """Check if a straight line between two world points crosses any blocked cell.

    Uses grid-based ray marching (DDA algorithm) for efficiency.
    """
    cs = nav_grid.cell_size
    ax, ay = a[0] / cs, a[1] / cs
    bx, by = b[0] / cs, b[1] / cs

    dx = bx - ax
    dy = by - ay
    steps = int(max(abs(dx), abs(dy)) * 2) + 1
    if steps == 0:
        return True

    sx = dx / steps
    sy = dy / steps

    blocked = nav_grid._blocked
    cols = nav_grid.cols
    rows = nav_grid.rows

    for step in range(steps + 1):
        px = ax + sx * step
        py = ay + sy * step
        cx = min(max(0, int(px)), cols - 1)
        cy = min(max(0, int(py)), rows - 1)
        if (cx, cy) in blocked:
            return False

    return True
