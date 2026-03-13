"""Obstacle generation, collision detection, and line-of-sight checks."""

from __future__ import annotations

import random
from typing import TYPE_CHECKING

from server.config import settings

if TYPE_CHECKING:
    from server.game.state import Obstacle


def generate_obstacles() -> list[Obstacle]:
    """Generate random rectangular obstacles for a new round."""
    from server.game.state import Obstacle

    count = random.randint(
        settings.arena_zone.obstacle_count_min,
        settings.arena_zone.obstacle_count_max,
    )
    obstacles: list[Obstacle] = []
    w = settings.game.arena_width
    h = settings.game.arena_height
    margin = 50.0  # keep obstacles away from edges

    for _ in range(count):
        ow = random.uniform(10.0, 60.0)
        oh = random.uniform(10.0, 60.0)
        ox = random.uniform(margin, w - margin - ow)
        oy = random.uniform(margin, h - margin - oh)
        obstacles.append(Obstacle(x=ox, y=oy, width=ow, height=oh))

    return obstacles


def collides_with_obstacle(
    x: float, y: float, obstacles: list[Obstacle], radius: float = 0.0,
) -> Obstacle | None:
    """Check if a point (or circle) overlaps any obstacle. Returns the obstacle or None.

    When radius > 0, the check treats the entity as a circle and expands
    each obstacle's bounding box by that radius in every direction.
    """
    for obs in obstacles:
        if (obs.x - radius <= x <= obs.x + obs.width + radius and
                obs.y - radius <= y <= obs.y + obs.height + radius):
            return obs
    return None


def slide_along_obstacle(
    old_x: float, old_y: float, new_x: float, new_y: float,
    obstacles: list[Obstacle], radius: float = 0.0,
) -> tuple[float, float]:
    """Attempt to move from old to new position, sliding along obstacles.

    When radius > 0, treats the entity as a circle and prevents any overlap.
    """
    # Try full move
    if collides_with_obstacle(new_x, new_y, obstacles, radius) is None:
        return (new_x, new_y)
    # Try moving only X
    if collides_with_obstacle(new_x, old_y, obstacles, radius) is None:
        return (new_x, old_y)
    # Try moving only Y
    if collides_with_obstacle(old_x, new_y, obstacles, radius) is None:
        return (old_x, new_y)
    # Blocked completely
    return (old_x, old_y)


def line_intersects_obstacle(
    x1: float, y1: float, x2: float, y2: float, obstacles: list[Obstacle]
) -> bool:
    """Check if a line segment from (x1,y1) to (x2,y2) intersects any obstacle.

    Uses parametric line-rectangle intersection and start/end point collision.
    """
    for obs in obstacles:
        # Check if endpoints are inside
        if (obs.x <= x1 <= obs.x + obs.width and obs.y <= y1 <= obs.y + obs.height) or \
           (obs.x <= x2 <= obs.x + obs.width and obs.y <= y2 <= obs.y + obs.height):
            return True
        # Check for edge crossing
        if _line_rect_intersect(x1, y1, x2, y2, obs):
            return True
    return False


def _line_rect_intersect(
    x1: float, y1: float, x2: float, y2: float, obs: Obstacle
) -> bool:
    """Check line segment intersection with a rectangle using slab method."""
    dx = x2 - x1
    dy = y2 - y1

    # Check all four edges
    edges = [
        (obs.x, obs.y, obs.x + obs.width, obs.y),              # top
        (obs.x, obs.y + obs.height, obs.x + obs.width, obs.y + obs.height),  # bottom
        (obs.x, obs.y, obs.x, obs.y + obs.height),              # left
        (obs.x + obs.width, obs.y, obs.x + obs.width, obs.y + obs.height),   # right
    ]

    for ex1, ey1, ex2, ey2 in edges:
        if _segments_intersect(x1, y1, x2, y2, ex1, ey1, ex2, ey2):
            return True
    return False


def _segments_intersect(
    ax1: float, ay1: float, ax2: float, ay2: float,
    bx1: float, by1: float, bx2: float, by2: float,
) -> bool:
    """Check if two line segments intersect using cross product method."""
    def cross(ox: float, oy: float, px: float, py: float, qx: float, qy: float) -> float:
        return (px - ox) * (qy - oy) - (py - oy) * (qx - ox)

    d1 = cross(bx1, by1, bx2, by2, ax1, ay1)
    d2 = cross(bx1, by1, bx2, by2, ax2, ay2)
    d3 = cross(ax1, ay1, ax2, ay2, bx1, by1)
    d4 = cross(ax1, ay1, ax2, ay2, bx2, by2)

    if ((d1 > 0 and d2 < 0) or (d1 < 0 and d2 > 0)) and \
       ((d3 > 0 and d4 < 0) or (d3 < 0 and d4 > 0)):
        return True
    return False
