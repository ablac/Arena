"""AI logic and configs for demo bots."""

import math
import random

# Weapon ranges for quick decision-making
WEAPON_RANGES = {
    "sword": 2.0, "bow": 20.0, "daggers": 1.5,
    "shield": 1.5, "spear": 3.0, "staff": 15.0,
}

# Demo bot configurations — mix of strategies and weapons
DEMO_CONFIGS = [
    {"name": "Demo-Berserker", "weapon": "sword", "stats": {"hp": 3, "speed": 4, "attack": 10, "defense": 3}, "strategy": "aggressive"},
    {"name": "Demo-Sniper", "weapon": "bow", "stats": {"hp": 2, "speed": 8, "attack": 7, "defense": 3}, "strategy": "kite"},
    {"name": "Demo-Tank", "weapon": "shield", "stats": {"hp": 10, "speed": 2, "attack": 3, "defense": 5}, "strategy": "territorial"},
    {"name": "Demo-Assassin", "weapon": "daggers", "stats": {"hp": 3, "speed": 9, "attack": 5, "defense": 3}, "strategy": "aggressive"},
    {"name": "Demo-Lancer", "weapon": "spear", "stats": {"hp": 4, "speed": 6, "attack": 6, "defense": 4}, "strategy": "aggressive"},
    {"name": "Demo-Mage", "weapon": "staff", "stats": {"hp": 3, "speed": 5, "attack": 7, "defense": 5}, "strategy": "kite"},
    {"name": "Demo-Guardian", "weapon": "shield", "stats": {"hp": 7, "speed": 3, "attack": 4, "defense": 6}, "strategy": "defensive"},
    {"name": "Demo-Ranger", "weapon": "bow", "stats": {"hp": 4, "speed": 7, "attack": 6, "defense": 3}, "strategy": "kite"},
    {"name": "Demo-Brawler", "weapon": "sword", "stats": {"hp": 5, "speed": 5, "attack": 7, "defense": 3}, "strategy": "aggressive"},
    {"name": "Demo-Phantom", "weapon": "daggers", "stats": {"hp": 2, "speed": 10, "attack": 6, "defense": 2}, "strategy": "kite"},
    {"name": "Demo-Warden", "weapon": "spear", "stats": {"hp": 6, "speed": 4, "attack": 5, "defense": 5}, "strategy": "territorial"},
    {"name": "Demo-Warlock", "weapon": "staff", "stats": {"hp": 4, "speed": 4, "attack": 8, "defense": 4}, "strategy": "defensive"},
    {"name": "Demo-Sentinel", "weapon": "shield", "stats": {"hp": 8, "speed": 3, "attack": 3, "defense": 6}, "strategy": "defensive"},
    {"name": "Demo-Duelist", "weapon": "daggers", "stats": {"hp": 4, "speed": 7, "attack": 6, "defense": 3}, "strategy": "aggressive"},
    {"name": "Demo-Marksman", "weapon": "bow", "stats": {"hp": 3, "speed": 6, "attack": 8, "defense": 3}, "strategy": "kite"},
]


def _distance(a: list | tuple, b: list | tuple) -> float:
    """Euclidean distance between two positions."""
    return math.hypot(a[0] - b[0], a[1] - b[1])


def _toward(src: list | tuple, dst: list | tuple) -> list[float]:
    """Normalized direction vector from src toward dst."""
    dx, dy = dst[0] - src[0], dst[1] - src[1]
    mag = math.hypot(dx, dy)
    return [dx / mag, dy / mag] if mag > 0 else [0.0, 0.0]


def _away(src: list | tuple, dst: list | tuple) -> list[float]:
    """Normalized direction vector from src away from dst."""
    d = _toward(src, dst)
    return [-d[0], -d[1]]


def _find_closest(pos: list | tuple, enemies: list) -> tuple[dict | None, float]:
    """Find the closest enemy and its distance."""
    closest, closest_dist = None, float("inf")
    for e in enemies:
        d = _distance(pos, e.get("position", [0, 0]))
        if d < closest_dist:
            closest_dist, closest = d, e
    return closest, closest_dist


def pick_action(strategy: str, state: dict, nearby: list, safe_zone: dict, weapon: str) -> dict:
    """Pick an action based on strategy and game state."""
    pos = state.get("position", [0, 0])
    hp = state.get("hp", 100)
    max_hp = state.get("max_hp", 100)
    wrange = WEAPON_RANGES.get(weapon, 2.0)

    enemies = [e for e in nearby if e.get("type") == "bot"]
    pickups = [e for e in nearby if e.get("type") == "pickup"]
    zone_center = safe_zone.get("center", [50, 50])
    zone_radius = safe_zone.get("radius", 100)

    closest, closest_dist = _find_closest(pos, enemies)

    # Low HP — grab nearby health pickup
    if hp < max_hp * 0.3 and pickups:
        nearest = min(pickups, key=lambda p: _distance(pos, p.get("position", [0, 0])))
        return {"action": "move", "direction": _toward(pos, nearest["position"])}

    # Outside safe zone — move toward center
    if _distance(pos, zone_center) > zone_radius * 0.8:
        return {"action": "move", "direction": _toward(pos, zone_center)}

    if strategy == "aggressive":
        if closest is None:
            return {"action": "move", "direction": _toward(pos, zone_center)}
        if closest_dist <= wrange:
            action = {"action": "attack", "target": closest["id"]}
            if weapon == "staff":
                action["direction"] = list(closest.get("position", pos))
            return action
        return {"action": "move", "direction": _toward(pos, closest["position"])}

    if strategy == "defensive":
        if closest is None:
            return {"action": "idle"}
        if closest_dist <= wrange:
            action = {"action": "attack", "target": closest["id"]}
            if weapon == "staff":
                action["direction"] = list(closest.get("position", pos))
            return action
        if closest_dist < wrange * 2:
            return {"action": "move", "direction": _away(pos, closest["position"])}
        return {"action": "idle"}

    if strategy == "kite":
        if closest is None:
            angle = random.uniform(0, 2 * math.pi)
            return {"action": "move", "direction": [math.cos(angle), math.sin(angle)]}
        if wrange * 0.3 < closest_dist <= wrange:
            action = {"action": "attack", "target": closest["id"]}
            if weapon == "staff":
                action["direction"] = list(closest.get("position", pos))
            return action
        if closest_dist < wrange * 0.4:
            if random.random() < 0.3:
                return {"action": "dodge", "direction": _away(pos, closest["position"])}
            return {"action": "move", "direction": _away(pos, closest["position"])}
        return {"action": "move", "direction": _toward(pos, closest["position"])}

    if strategy == "territorial":
        if closest is None:
            return {"action": "idle"}
        if closest_dist <= wrange:
            action = {"action": "attack", "target": closest["id"]}
            if weapon == "staff":
                action["direction"] = list(closest.get("position", pos))
            return action
        if closest_dist <= wrange * 3:
            return {"action": "move", "direction": _toward(pos, closest["position"])}
        return {"action": "idle"}

    return {"action": "idle"}
