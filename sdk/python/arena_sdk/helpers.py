"""Helper functions for distance, direction, and entity filtering."""

from __future__ import annotations

import math
from typing import Any


def _x(pos: dict | tuple) -> float:
    """Extract x coordinate from dict or tuple."""
    return pos["x"] if isinstance(pos, dict) else pos[0]


def _y(pos: dict | tuple) -> float:
    """Extract y coordinate from dict or tuple."""
    return pos["y"] if isinstance(pos, dict) else pos[1]


def distance(pos_a: dict | tuple, pos_b: dict | tuple) -> float:
    """Euclidean distance between two positions (dict {x,y} or tuple)."""
    dx = _x(pos_a) - _x(pos_b)
    dy = _y(pos_a) - _y(pos_b)
    return math.hypot(dx, dy)


def normalize(dx: float, dy: float) -> dict[str, float]:
    """Normalize a direction vector. Returns {x, y}."""
    mag = math.hypot(dx, dy)
    if mag == 0:
        return {"x": 0.0, "y": 0.0}
    return {"x": dx / mag, "y": dy / mag}


def direction_toward(from_pos: dict | tuple, to_pos: dict | tuple) -> dict[str, float]:
    """Return normalized direction from from_pos toward to_pos."""
    dx = _x(to_pos) - _x(from_pos)
    dy = _y(to_pos) - _y(from_pos)
    return normalize(dx, dy)


def direction_away(from_pos: dict | tuple, to_pos: dict | tuple) -> dict[str, float]:
    """Return normalized direction from from_pos away from to_pos."""
    dx = _x(from_pos) - _x(to_pos)
    dy = _y(from_pos) - _y(to_pos)
    return normalize(dx, dy)


def closest_entity(
    my_pos: dict | tuple, entities: list[dict[str, Any]]
) -> dict[str, Any] | None:
    """Find the nearest entity to my_pos. Returns None if list is empty."""
    if not entities:
        return None
    return min(entities, key=lambda e: distance(my_pos, e.get("position", e)))


def lowest_hp_entity(entities: list[dict[str, Any]]) -> dict[str, Any] | None:
    """Find entity with the lowest hp. Returns None if list is empty."""
    if not entities:
        return None
    return min(entities, key=lambda e: e.get("hp", float("inf")))


def filter_by_type(
    entities: list[dict[str, Any]], entity_type: str
) -> list[dict[str, Any]]:
    """Filter entities by type ('bot', 'pickup', etc.)."""
    return [e for e in entities if e.get("type") == entity_type]
