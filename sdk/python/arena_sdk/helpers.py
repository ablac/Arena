"""Helper functions for distance, direction, and entity filtering."""

from __future__ import annotations

from typing import Any


def _x(pos: dict | tuple | list) -> int | float:
    """Extract x (col) coordinate from dict or list/tuple."""
    return pos["x"] if isinstance(pos, dict) else pos[0]


def _y(pos: dict | tuple | list) -> int | float:
    """Extract y (row) coordinate from dict or list/tuple."""
    return pos["y"] if isinstance(pos, dict) else pos[1]


def distance(pos_a: dict | tuple | list, pos_b: dict | tuple | list) -> int:
    """Chebyshev distance between two grid positions.

    For grid-based maps, this is max(|dx|, |dy|) — the number of moves
    required when diagonal movement is allowed.
    """
    dx = abs(_x(pos_a) - _x(pos_b))
    dy = abs(_y(pos_a) - _y(pos_b))
    return max(dx, dy)


def _sign(n: int | float) -> int:
    """Return -1, 0, or 1 based on sign of n."""
    if n > 0:
        return 1
    elif n < 0:
        return -1
    return 0


def direction_toward(
    from_pos: dict | tuple | list, to_pos: dict | tuple | list
) -> dict[str, int]:
    """Return grid direction (-1/0/1 per axis) from from_pos toward to_pos."""
    dx = _x(to_pos) - _x(from_pos)
    dy = _y(to_pos) - _y(from_pos)
    return {"x": _sign(dx), "y": _sign(dy)}


def direction_away(
    from_pos: dict | tuple | list, to_pos: dict | tuple | list
) -> dict[str, int]:
    """Return grid direction (-1/0/1 per axis) from from_pos away from to_pos."""
    dx = _x(from_pos) - _x(to_pos)
    dy = _y(from_pos) - _y(to_pos)
    return {"x": _sign(dx), "y": _sign(dy)}


def closest_entity(
    my_pos: dict | tuple | list, entities: list[dict[str, Any]]
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
