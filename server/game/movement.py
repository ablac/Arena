"""Movement processing for the game engine tick loop."""

from __future__ import annotations

import math
from typing import TYPE_CHECKING

from server.config import settings
from server.game.obstacles import slide_along_obstacle
from server.game.pickups import get_effective_speed
from server.game.state import ActionType

if TYPE_CHECKING:
    from server.game.arena_map import ArenaMap
    from server.game.spatial import SpatialGrid
    from server.game.state import BotState, Obstacle


def process_movement(
    bots: dict[str, BotState],
    arena: ArenaMap,
    grid: SpatialGrid,
    obstacles: list[Obstacle],
) -> None:
    """Process move and dodge actions, updating positions and spatial grid."""
    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.pending_action is None:
            continue
        if bot.stun_ticks > 0:
            continue

        action = bot.pending_action.action_type

        if action == ActionType.DODGE:
            _process_dodge(bot_id, bot, arena, grid, obstacles)
        elif action == ActionType.MOVE:
            _process_move(bot_id, bot, arena, grid, obstacles)


def _process_move(
    bot_id: str, bot: BotState, arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    """Process a normal move action."""
    direction = bot.pending_action.direction
    if direction is None:
        return

    dx, dy = direction
    length = math.sqrt(dx * dx + dy * dy)
    if length == 0:
        return
    dx /= length
    dy /= length

    speed = get_effective_speed(bot)
    new_x = bot.position[0] + dx * speed
    new_y = bot.position[1] + dy * speed

    # Obstacle collision (slide along edges)
    new_x, new_y = slide_along_obstacle(
        bot.position[0], bot.position[1], new_x, new_y, obstacles
    )
    new_x, new_y = arena.clamp_position(new_x, new_y)

    # Track distance
    old_x, old_y = bot.position
    dist = math.sqrt((new_x - old_x) ** 2 + (new_y - old_y) ** 2)
    bot.round_distance += dist

    bot.position = (new_x, new_y)
    grid.update(bot_id, new_x, new_y)


def _process_dodge(
    bot_id: str, bot: BotState, arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    """Process a dodge action — 2x speed dash with invulnerability."""
    if bot.dodge_cooldown > 0:
        return  # Still on cooldown

    direction = bot.pending_action.direction
    if direction is None:
        return

    dx, dy = direction
    length = math.sqrt(dx * dx + dy * dy)
    if length == 0:
        return
    dx /= length
    dy /= length

    dodge_speed = bot.speed * settings.combat.dodge_speed_mult
    new_x = bot.position[0] + dx * dodge_speed
    new_y = bot.position[1] + dy * dodge_speed

    new_x, new_y = slide_along_obstacle(
        bot.position[0], bot.position[1], new_x, new_y, obstacles
    )
    new_x, new_y = arena.clamp_position(new_x, new_y)

    old_x, old_y = bot.position
    dist = math.sqrt((new_x - old_x) ** 2 + (new_y - old_y) ** 2)
    bot.round_distance += dist

    bot.position = (new_x, new_y)
    grid.update(bot_id, new_x, new_y)

    # Apply invulnerability and cooldown
    bot.invuln_ticks = settings.combat.dodge_invuln_ticks
    bot.dodge_cooldown = settings.combat.dodge_cooldown_ticks
