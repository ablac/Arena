"""Pure-logic helpers extracted from GameEngine to keep engine.py under 200 lines."""

from __future__ import annotations

from typing import TYPE_CHECKING

from server.game.fallback_ai import get_fallback_action
from server.game.pickups import collect_by_action
from server.game.state import ActionType

if TYPE_CHECKING:
    from server.game.arena_map import ArenaMap
    from server.game.kill_feed import KillFeed
    from server.game.state import BotState, Pickup


def apply_fallbacks(
    bots: dict[str, BotState], get_nearby_fn,
) -> None:
    """Assign fallback AI actions to bots with no pending action."""
    for bot in bots.values():
        if not bot.is_alive or bot.pending_action is not None or bot.stun_ticks > 0:
            continue
        nearby = get_nearby_fn(bot)
        bot.pending_action = get_fallback_action(bot, nearby, bot.fallback_behavior)


def process_use_items(bots: dict[str, BotState], pickups: list[Pickup]) -> None:
    """Process USE_ITEM actions — collect pickups by action."""
    for bot in bots.values():
        if not bot.is_alive or bot.pending_action is None:
            continue
        if bot.pending_action.action_type == ActionType.USE_ITEM and bot.pending_action.item_id:
            collect_by_action(bot, bot.pending_action.item_id, pickups)


def apply_zone_damage(bots: dict[str, BotState], arena: ArenaMap) -> None:
    """Damage bots outside the safe zone."""
    for bot in bots.values():
        if bot.is_alive and not arena.is_in_safe_zone(*bot.position):
            bot.hp -= arena.damage_per_tick


def handle_kill_credits(
    death_events: list[dict], bots: dict[str, BotState],
    kill_feed: KillFeed, tick_count: int,
) -> None:
    """Attribute kills to attackers, update kill streaks and kill feed."""
    for event in death_events:
        bid = event["bot_id"]
        dead = bots.get(bid)
        if dead:
            dead.round_deaths += 1
        for other in bots.values():
            if other.bot_id == bid or not other.is_alive:
                continue
            act = other.pending_action
            if act and act.action_type == ActionType.ATTACK and act.target_id == bid:
                other.kill_streak += 1
                other.round_kills += 1
                event.update(killed_by=other.bot_id, killer_name=other.name, weapon=other.weapon)
                kill_feed.add_kill(other.name, dead.name if dead else "?", other.weapon, tick_count)
                break


def update_life_tracking(bots: dict[str, BotState], tick_count: int) -> None:
    """Track longest life per round for each bot."""
    for bot in bots.values():
        if bot.is_alive and bot.round_life_start_tick > 0:
            life = tick_count - bot.round_life_start_tick
            if life > bot.round_longest_life:
                bot.round_longest_life = life
