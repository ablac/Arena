"""Projectile tracking for bow arrows."""

from __future__ import annotations

import logging
import math
from typing import TYPE_CHECKING

from server.config import settings
from server.game.damage import apply_damage
from server.game.obstacles import line_intersects_obstacle

if TYPE_CHECKING:
    from server.game.state import BotState, Obstacle, Projectile

logger = logging.getLogger(__name__)

_next_id = 0


def create_projectile(
    owner: BotState, target_pos: tuple[float, float], damage: float
) -> Projectile:
    """Create a new projectile from owner toward target position."""
    from server.game.state import Projectile

    global _next_id
    _next_id += 1

    dx = target_pos[0] - owner.position[0]
    dy = target_pos[1] - owner.position[1]
    dist = math.sqrt(dx * dx + dy * dy)
    if dist == 0:
        direction = (1.0, 0.0)
    else:
        direction = (dx / dist, dy / dist)

    speed = settings.combat.projectile_speed
    max_age = int(settings.combat.projectile_max_age_secs * settings.game.tick_rate)

    return Projectile(
        projectile_id=f"arrow_{_next_id}",
        owner_id=owner.bot_id,
        position=owner.position,
        direction=direction,
        speed=speed,
        damage=damage,
        weapon="bow",
        max_age_ticks=max_age,
    )


def update_projectiles(
    projectiles: list[Projectile],
    bots: dict[str, BotState],
    obstacles: list[Obstacle],
    tick_rate: int,
) -> list[dict]:
    """Move projectiles, check hits, return hit events. Removes expired/hit projectiles."""
    hit_radius = settings.combat.projectile_hit_radius + settings.game.bot_radius
    hit_radius_sq = hit_radius * hit_radius
    dt = 1.0 / tick_rate
    events: list[dict] = []
    to_remove: list[int] = []

    for i, proj in enumerate(projectiles):
        proj.age_ticks += 1
        old_pos = proj.position

        # Move projectile
        move_dist = proj.speed * dt
        new_x = proj.position[0] + proj.direction[0] * move_dist
        new_y = proj.position[1] + proj.direction[1] * move_dist
        proj.position = (new_x, new_y)

        # Check obstacle collision (LOS blocked)
        if line_intersects_obstacle(old_pos[0], old_pos[1], new_x, new_y, obstacles):
            to_remove.append(i)
            continue

        # Check arena bounds
        if new_x < 0 or new_x > settings.game.arena_width or \
           new_y < 0 or new_y > settings.game.arena_height:
            to_remove.append(i)
            continue

        # Check hit against bots
        hit = False
        for bot_id, bot in bots.items():
            if bot_id == proj.owner_id or not bot.is_alive:
                continue
            dx = bot.position[0] - new_x
            dy = bot.position[1] - new_y
            if dx * dx + dy * dy <= hit_radius_sq:
                # Invulnerable targets consume the projectile but take no damage
                if bot.invuln_ticks > 0:
                    to_remove.append(i)
                    hit = True
                    break
                # Use apply_damage for proper stat tracking and kill attribution
                apply_damage(bots, bot_id, proj.damage, proj.owner_id, proj.weapon, events)
                to_remove.append(i)
                hit = True
                break

        if hit:
            continue

        # Check age
        if proj.age_ticks >= proj.max_age_ticks:
            to_remove.append(i)

    # Remove in reverse order
    for i in reversed(to_remove):
        projectiles.pop(i)

    return events
