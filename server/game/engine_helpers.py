"""Pure-logic helpers extracted from GameEngine to keep engine.py under 200 lines."""

from __future__ import annotations

from typing import TYPE_CHECKING

from server.game.elo import apply_elo_change
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
) -> list[dict]:
    """Attribute kills via last_damaged_by, update kill streaks, ELO, and kill feed.

    Returns a list of kill events to send to killer bots.
    """
    kill_events: list[dict] = []
    for event in death_events:
        bid = event["bot_id"]
        dead = bots.get(bid)
        if not dead:
            continue
        dead.round_deaths += 1

        # Use last_damaged_by for reliable kill attribution
        killer_id = dead.last_damaged_by
        killer = bots.get(killer_id) if killer_id else None
        if killer and killer.is_alive and killer.bot_id != bid:
            killer.kill_streak += 1
            killer.round_kills += 1
            event.update(killed_by=killer.bot_id, killer_name=killer.name, weapon=killer.weapon)
            kill_feed.add_kill(killer.name, dead.name, killer.weapon, tick_count)

            # Update ELO ratings
            new_killer_elo, new_victim_elo = apply_elo_change(killer.elo, dead.elo)
            killer.elo = new_killer_elo
            dead.elo = new_victim_elo

            kill_events.append({
                "bot_id": killer.bot_id,
                "victim_name": dead.name,
                "victim_id": dead.bot_id,
                "weapon": killer.weapon,
                "kill_streak": killer.kill_streak,
                "round_kills": killer.round_kills,
            })

        dead.last_damaged_by = None
    return kill_events


def update_life_tracking(bots: dict[str, BotState], tick_count: int) -> None:
    """Track longest life per round for each bot."""
    for bot in bots.values():
        if bot.is_alive and bot.round_life_start_tick > 0:
            life = tick_count - bot.round_life_start_tick
            if life > bot.round_longest_life:
                bot.round_longest_life = life
