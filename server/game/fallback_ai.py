"""Fallback AI for bots that don't respond within the tick window."""

from __future__ import annotations

import math
from typing import TYPE_CHECKING

from server.config import settings
from server.game.state import Action, ActionType
from server.game.weapons import get_weapon_config

if TYPE_CHECKING:
    from server.game.state import BotState


def _distance(a: tuple[float, float], b: tuple[float, float]) -> float:
    """Euclidean distance between two positions."""
    dx = a[0] - b[0]
    dy = a[1] - b[1]
    return math.sqrt(dx * dx + dy * dy)


def _direction_toward(
    from_pos: tuple[float, float], to_pos: tuple[float, float]
) -> tuple[float, float]:
    """Normalized direction vector from one position toward another."""
    dx = to_pos[0] - from_pos[0]
    dy = to_pos[1] - from_pos[1]
    dist = math.sqrt(dx * dx + dy * dy)
    if dist == 0:
        return (0.0, 0.0)
    return (dx / dist, dy / dist)


def _direction_away(
    from_pos: tuple[float, float], threat_pos: tuple[float, float]
) -> tuple[float, float]:
    """Normalized direction vector away from a threat."""
    toward = _direction_toward(from_pos, threat_pos)
    return (-toward[0], -toward[1])


def _arena_center() -> tuple[float, float]:
    return (float(settings.game.arena_width) / 2, float(settings.game.arena_height) / 2)


def _find_nearest(bot: BotState, nearby: list[BotState]) -> BotState | None:
    """Find the nearest alive bot from the nearby list."""
    best, best_dist = None, float("inf")
    for other in nearby:
        d = _distance(bot.position, other.position)
        if d < best_dist:
            best, best_dist = other, d
    return best


def _roam_toward_center(bot: BotState) -> Action:
    """Move toward arena center when no enemies are visible."""
    d = _direction_toward(bot.position, _arena_center())
    if d == (0.0, 0.0):
        return Action(action_type=ActionType.IDLE)
    return Action(action_type=ActionType.MOVE, direction=d)


def _find_lowest_hp(nearby: list[BotState]) -> BotState | None:
    """Find the nearby bot with the lowest HP."""
    if not nearby:
        return None
    return min(nearby, key=lambda b: b.hp)


def _find_highest_streak(nearby: list[BotState]) -> BotState | None:
    """Find the nearby bot with the highest kill streak."""
    if not nearby:
        return None
    return max(nearby, key=lambda b: b.kill_streak)


def _can_attack(bot: BotState, target: BotState) -> bool:
    """Check if bot can attack target (in range and off cooldown)."""
    if bot.cooldown_remaining > 0:
        return False
    cfg = get_weapon_config(bot.weapon)
    return _distance(bot.position, target.position) <= cfg["range"]


def get_fallback_action(
    bot: BotState, nearby: list[BotState], behavior: str
) -> Action:
    """Generate a fallback action based on the bot's configured behavior.

    Args:
        bot: The bot needing a fallback action.
        nearby: Alive bots within view radius (excluding self).
        behavior: One of aggressive, defensive, opportunistic, territorial, hunter.
    """
    handlers = {
        "aggressive": _aggressive,
        "defensive": _defensive,
        "opportunistic": _opportunistic,
        "territorial": _territorial,
        "hunter": _hunter,
    }
    handler = handlers.get(behavior, _aggressive)
    return handler(bot, nearby)


def _aggressive(bot: BotState, nearby: list[BotState]) -> Action:
    """Move toward nearest enemy, attack if in range."""
    target = _find_nearest(bot, nearby)
    if target is None:
        return _roam_toward_center(bot)
    if _can_attack(bot, target):
        return Action(action_type=ActionType.ATTACK, target_id=target.bot_id)
    return Action(
        action_type=ActionType.MOVE,
        direction=_direction_toward(bot.position, target.position),
    )


def _defensive(bot: BotState, nearby: list[BotState]) -> Action:
    """Move away from nearest enemy, attack only if in weapon range."""
    target = _find_nearest(bot, nearby)
    if target is None:
        return _roam_toward_center(bot)
    if _can_attack(bot, target):
        return Action(action_type=ActionType.ATTACK, target_id=target.bot_id)
    return Action(
        action_type=ActionType.MOVE,
        direction=_direction_away(bot.position, target.position),
    )


def _opportunistic(bot: BotState, nearby: list[BotState]) -> Action:
    """Attack lowest-HP enemy; flee from enemies with >70% HP."""
    weak = [b for b in nearby if b.hp <= b.max_hp * 0.7]
    if weak:
        target = _find_lowest_hp(weak)
        if target and _can_attack(bot, target):
            return Action(action_type=ActionType.ATTACK, target_id=target.bot_id)
        if target:
            return Action(
                action_type=ActionType.MOVE,
                direction=_direction_toward(bot.position, target.position),
            )
    # Flee from strong enemies
    strong = _find_nearest(bot, [b for b in nearby if b.hp > b.max_hp * 0.7])
    if strong:
        return Action(
            action_type=ActionType.MOVE,
            direction=_direction_away(bot.position, strong.position),
        )
    return _roam_toward_center(bot)


def _territorial(bot: BotState, nearby: list[BotState]) -> Action:
    """Stay near spawn, attack anything within 2x weapon range."""
    cfg = get_weapon_config(bot.weapon)
    territory_range = cfg["range"] * 2
    # Attack nearby threats
    for target in nearby:
        if _distance(bot.position, target.position) <= territory_range:
            if _can_attack(bot, target):
                return Action(action_type=ActionType.ATTACK, target_id=target.bot_id)
    # Return to spawn if drifted
    if _distance(bot.position, bot.spawn_position) > territory_range:
        return Action(
            action_type=ActionType.MOVE,
            direction=_direction_toward(bot.position, bot.spawn_position),
        )
    return Action(action_type=ActionType.IDLE)


def _hunter(bot: BotState, nearby: list[BotState]) -> Action:
    """Move toward highest-streak bot, attack when in range."""
    target = _find_highest_streak(nearby)
    if target is None:
        return _roam_toward_center(bot)
    if _can_attack(bot, target):
        return Action(action_type=ActionType.ATTACK, target_id=target.bot_id)
    return Action(
        action_type=ActionType.MOVE,
        direction=_direction_toward(bot.position, target.position),
    )
