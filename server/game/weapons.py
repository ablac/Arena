"""Weapon definitions and damage calculation."""

from __future__ import annotations

import random
from typing import TYPE_CHECKING

from server.config import settings
from server.game.pickups import get_effective_damage_mult

if TYPE_CHECKING:
    from server.game.state import BotState


def get_weapon_config(weapon_name: str) -> dict:
    """Get weapon configuration dict from settings.

    Falls back to sword if weapon not found.
    """
    weapon_cfg = getattr(settings.weapons, weapon_name, None)
    if weapon_cfg is None:
        weapon_cfg = settings.weapons.sword
    return {
        "damage": weapon_cfg.damage,
        "range": weapon_cfg.range,
        "cooldown": weapon_cfg.cooldown,
        "special": weapon_cfg.special,
        "special_param": weapon_cfg.special_param,
    }


def get_available_weapons() -> list[str]:
    """Return list of available weapon names."""
    return [
        name
        for name in ["sword", "bow", "daggers", "shield", "spear", "staff"]
        if hasattr(settings.weapons, name)
    ]


def is_ranged(weapon_name: str) -> bool:
    """Check if a weapon requires line-of-sight checks."""
    return weapon_name in ("bow", "staff")


def calculate_damage(weapon_name: str, attacker: BotState, target: BotState) -> float:
    """Calculate damage from attacker to target.

    Includes damage boost effects and shield passive.
    """
    cfg = get_weapon_config(weapon_name)
    eff_mult = get_effective_damage_mult(attacker)
    damage = cfg["damage"] * eff_mult * (1.0 - target.defense_reduction)

    # Shield passive block (50% reduction)
    if target.weapon == "shield":
        damage *= 0.5

    return max(1.0, damage)


def is_in_range(attacker: BotState, target: BotState, weapon_name: str) -> bool:
    """Check if target is within weapon range of attacker."""
    cfg = get_weapon_config(weapon_name)
    dx = attacker.position[0] - target.position[0]
    dy = attacker.position[1] - target.position[1]
    return dx * dx + dy * dy <= cfg["range"] ** 2


def apply_cleave(
    attacker: BotState, primary_target: BotState, all_bots: dict[str, BotState]
) -> list[tuple[str, float]]:
    """Sword cleave: 50% damage to all enemies within sword range."""
    extra: list[tuple[str, float]] = []
    for bid, bot in all_bots.items():
        if bid in (attacker.bot_id, primary_target.bot_id) or not bot.is_alive:
            continue
        if is_in_range(attacker, bot, "sword"):
            dmg = calculate_damage("sword", attacker, bot) * 0.5
            extra.append((bid, dmg))
    return extra


def apply_double_strike(
    attacker: BotState, target: BotState
) -> list[tuple[str, float]]:
    """Daggers: 20% chance to strike twice."""
    cfg = get_weapon_config("daggers")
    if random.random() < cfg["special_param"]:
        return [(target.bot_id, calculate_damage("daggers", attacker, target))]
    return []


def apply_knockback(
    attacker: BotState, target: BotState, obstacles: list
) -> int:
    """Spear: push target 2 units away. Returns bonus wall damage."""
    cfg = get_weapon_config("spear")
    push_dist = cfg["special_param"]
    dx = target.position[0] - attacker.position[0]
    dy = target.position[1] - attacker.position[1]
    dist = (dx * dx + dy * dy) ** 0.5
    if dist == 0:
        return 0

    nx, ny = dx / dist, dy / dist
    new_x = target.position[0] + nx * push_dist
    new_y = target.position[1] + ny * push_dist

    w, h = float(settings.game.arena_width), float(settings.game.arena_height)
    clamped_x = max(0.0, min(w, new_x))
    clamped_y = max(0.0, min(h, new_y))

    # Check wall/obstacle hit for bonus damage
    hit_wall = (clamped_x != new_x or clamped_y != new_y)
    from server.game.obstacles import collides_with_obstacle
    hit_obs = collides_with_obstacle(new_x, new_y, obstacles) is not None

    if hit_obs:
        from server.game.obstacles import slide_along_obstacle
        clamped_x, clamped_y = slide_along_obstacle(
            target.position[0], target.position[1], new_x, new_y, obstacles
        )

    target.position = (clamped_x, clamped_y)
    bonus = settings.combat.knockback_wall_damage if (hit_wall or hit_obs) else 0
    return bonus
