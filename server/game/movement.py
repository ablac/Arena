"""Movement processing for the game engine tick loop."""

from __future__ import annotations

import math
from typing import TYPE_CHECKING

from server.config import settings
from server.game.obstacles import collides_with_obstacle, slide_along_obstacle
from server.game.pathfinding import NavGrid, find_path
from server.game.pickups import get_effective_speed
from server.game.state import ActionType

if TYPE_CHECKING:
    from server.game.arena_map import ArenaMap
    from server.game.spatial import SpatialGrid
    from server.game.state import BotState, Obstacle

_nav_grid: NavGrid | None = None
_nav_grid_obstacle_id: int | None = None

BOT_SEPARATION_DIST = settings.game.bot_radius * 2  # surface-to-surface = 0
_SEP_QUERY_R = BOT_SEPARATION_DIST + 5.0


def _get_nav_grid(obstacles: list[Obstacle]) -> NavGrid:
    global _nav_grid, _nav_grid_obstacle_id
    obs_id = id(obstacles)
    if _nav_grid is None or _nav_grid_obstacle_id != obs_id:
        _nav_grid = NavGrid()
        _nav_grid.build(obstacles)
        _nav_grid_obstacle_id = obs_id
    return _nav_grid


def reset_nav_grid() -> None:
    """Force the navigation grid to rebuild on next access."""
    global _nav_grid, _nav_grid_obstacle_id
    _nav_grid = None
    _nav_grid_obstacle_id = None


def _apply_move(
    bot_id: str, bot: BotState, new_x: float, new_y: float,
    arena: ArenaMap, grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    """Slide against obstacles, clamp, track distance, commit position."""
    new_x, new_y = slide_along_obstacle(
        bot.position[0], bot.position[1], new_x, new_y, obstacles,
        radius=settings.game.bot_radius,
    )
    new_x, new_y = arena.clamp_position(new_x, new_y)
    old_x, old_y = bot.position
    bot.round_distance += math.sqrt((new_x - old_x) ** 2 + (new_y - old_y) ** 2)
    bot.position = (new_x, new_y)
    grid.update(bot_id, new_x, new_y)


def _normalize(dx: float, dy: float) -> tuple[float, float] | None:
    length = math.sqrt(dx * dx + dy * dy)
    if length == 0:
        return None
    return dx / length, dy / length


def process_movement(
    bots: dict[str, BotState], arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    """Process move/move_to/dodge actions, then separate overlapping bots."""
    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.pending_action is None or bot.stun_ticks > 0:
            continue
        action = bot.pending_action.action_type
        if action == ActionType.DODGE:
            _process_dodge(bot_id, bot, arena, grid, obstacles)
        elif action == ActionType.MOVE:
            _process_move(bot_id, bot, arena, grid, obstacles)
        elif action == ActionType.MOVE_TO:
            _process_move_to(bot_id, bot, arena, grid, obstacles)
    separate_bots(bots, arena, grid)


def _process_move(
    bot_id: str, bot: BotState, arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    d = bot.pending_action.direction
    if d is None:
        return
    n = _normalize(d[0], d[1])
    if n is None:
        return
    speed = get_effective_speed(bot)
    _apply_move(bot_id, bot,
                bot.position[0] + n[0] * speed,
                bot.position[1] + n[1] * speed,
                arena, grid, obstacles)


def _process_move_to(
    bot_id: str, bot: BotState, arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    target = bot.pending_action.target_position
    if target is None:
        return
    if bot.path_target != target or not bot.current_path:
        bot.current_path = find_path(bot.position, target, _get_nav_grid(obstacles))
        bot.path_target = target
        if not bot.current_path:
            bot.path_target = None
            return

    speed = get_effective_speed(bot)
    wx, wy = bot.current_path[0]
    dx, dy = wx - bot.position[0], wy - bot.position[1]
    dist_wp = math.sqrt(dx * dx + dy * dy)

    if dist_wp <= speed:
        new_x, new_y = wx, wy
        bot.current_path.pop(0)
        if not bot.current_path:
            bot.path_target = None
    else:
        dx /= dist_wp
        dy /= dist_wp
        new_x = bot.position[0] + dx * speed
        new_y = bot.position[1] + dy * speed

    _apply_move(bot_id, bot, new_x, new_y, arena, grid, obstacles)


def _process_dodge(
    bot_id: str, bot: BotState, arena: ArenaMap,
    grid: SpatialGrid, obstacles: list[Obstacle],
) -> None:
    if bot.dodge_cooldown > 0:
        return
    d = bot.pending_action.direction
    if d is None:
        return
    n = _normalize(d[0], d[1])
    if n is None:
        return
    speed = bot.speed * settings.combat.dodge_speed_mult
    _apply_move(bot_id, bot,
                bot.position[0] + n[0] * speed,
                bot.position[1] + n[1] * speed,
                arena, grid, obstacles)
    bot.invuln_ticks = settings.combat.dodge_invuln_ticks
    bot.dodge_cooldown = settings.combat.dodge_cooldown_ticks
    bot.last_action = "dodge"


def separate_bots(
    bots: dict[str, BotState], arena: ArenaMap, grid: SpatialGrid,
) -> None:
    """Push apart bots closer than BOT_SEPARATION_DIST (2 iterations)."""
    for _ in range(2):
        for bot_id, bot in bots.items():
            if not bot.is_alive:
                continue
            nearby = grid.query_radius(bot.position[0], bot.position[1], _SEP_QUERY_R)
            for oid in nearby:
                if oid == bot_id or oid not in bots:
                    continue
                other = bots[oid]
                if not other.is_alive:
                    continue
                dx = bot.position[0] - other.position[0]
                dy = bot.position[1] - other.position[1]
                dist = math.sqrt(dx * dx + dy * dy)
                if dist >= BOT_SEPARATION_DIST or dist == 0:
                    continue
                nx, ny = dx / dist, dy / dist
                push = (BOT_SEPARATION_DIST - dist) * 0.6
                bot_r = settings.game.bot_radius
                bx = bot.position[0] + nx * push
                by = bot.position[1] + ny * push
                if collides_with_obstacle(bx, by, arena.obstacles, bot_r) is None:
                    bx, by = arena.clamp_position(bx, by)
                    bot.position = (bx, by)
                    grid.update(bot_id, bx, by)
                ox = other.position[0] - nx * push
                oy = other.position[1] - ny * push
                if collides_with_obstacle(ox, oy, arena.obstacles, bot_r) is None:
                    ox, oy = arena.clamp_position(ox, oy)
                    other.position = (ox, oy)
                    grid.update(oid, ox, oy)
