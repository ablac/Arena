"""Spawn, respawn, and death handling for the game engine."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from server.config import settings

if TYPE_CHECKING:
    from server.game.arena_map import ArenaMap
    from server.game.spatial import SpatialGrid
    from server.game.state import BotState

logger = logging.getLogger(__name__)


def spawn_bot(bot: BotState, arena: ArenaMap, grid: SpatialGrid) -> None:
    """Place a bot at a random position in the safe zone."""
    pos = arena.get_random_spawn_point()
    bot.position = pos
    bot.spawn_position = pos
    bot.is_alive = True
    bot.hp = bot.max_hp
    bot.cooldown_remaining = 0.0
    bot.respawn_timer = 0.0
    bot.active_effects.clear()
    bot.dodge_cooldown = 0
    bot.invuln_ticks = 0
    bot.stun_ticks = 0
    bot.shield_absorb = 0
    bot.current_path.clear()
    bot.path_target = None
    grid.insert(bot.bot_id, pos[0], pos[1])
    logger.info("Bot %s spawned at (%.1f, %.1f)", bot.name, pos[0], pos[1])


def check_deaths(
    bots: dict[str, BotState],
    grid: SpatialGrid,
    tick_count: int,
) -> list[dict]:
    """Check for dead bots (hp <= 0), emit death events, start respawn timers.

    Returns list of death event dicts.
    """
    events: list[dict] = []
    respawn_time = settings.combat.respawn_time

    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.hp > 0:
            continue

        bot.is_alive = False
        bot.hp = 0
        bot.respawn_timer = float(respawn_time)
        bot.current_path.clear()
        bot.path_target = None
        grid.remove(bot_id)

        events.append({
            "type": "death",
            "bot_id": bot_id,
            "bot_name": bot.name,
            "kills_this_life": bot.kill_streak,
            "tick": tick_count,
        })

        bot.kill_streak = 0
        logger.info("Bot %s died at tick %d", bot.name, tick_count)

    return events


def process_respawns(
    bots: dict[str, BotState],
    arena: ArenaMap,
    grid: SpatialGrid,
    tick_rate: int,
) -> list[dict]:
    """Tick down respawn timers and respawn bots when ready.

    Returns list of respawn event dicts.
    """
    events: list[dict] = []
    dt = 1.0 / tick_rate

    for bot_id, bot in bots.items():
        if bot.is_alive or bot.respawn_timer <= 0:
            continue

        bot.respawn_timer -= dt
        if bot.respawn_timer <= 0:
            bot.respawn_timer = 0.0
            pos = arena.get_random_spawn_point()
            bot.position = pos
            bot.spawn_position = pos
            bot.is_alive = True
            bot.hp = bot.max_hp
            bot.cooldown_remaining = 0.0
            bot.active_effects.clear()
            bot.dodge_cooldown = 0
            bot.invuln_ticks = 0
            bot.stun_ticks = 0
            bot.shield_absorb = 0
            bot.current_path.clear()
            bot.path_target = None
            bot.last_damaged_by = None
            grid.insert(bot_id, pos[0], pos[1])

            events.append({
                "type": "respawn",
                "bot_id": bot_id,
                "position": pos,
                "hp": bot.hp,
            })
            logger.info("Bot %s respawned at (%.1f, %.1f)", bot.name, pos[0], pos[1])

    return events
