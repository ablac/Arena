"""Pickup spawning, collection, and effect management."""

from __future__ import annotations

import logging
import random
from typing import TYPE_CHECKING

from server.config import settings
from server.game.state import Effect, Pickup

if TYPE_CHECKING:
    from server.game.arena_map import ArenaMap
    from server.game.state import BotState

logger = logging.getLogger(__name__)

_PICKUP_TYPES = ["health_pack", "speed_boost", "damage_boost", "shield_bubble"]
_next_id = 0


def maybe_spawn_pickup(
    pickups: list[Pickup], arena: ArenaMap, tick_count: int
) -> Pickup | None:
    """Spawn a pickup every spawn_interval ticks if under max."""
    if len(pickups) >= settings.pickups.max_active:
        return None
    if tick_count % settings.pickups.spawn_interval_ticks != 0:
        return None

    global _next_id
    _next_id += 1
    ptype = random.choice(_PICKUP_TYPES)
    pos = arena.get_random_spawn_point()

    pickup = Pickup(
        pickup_id=f"pickup_{_next_id}",
        pickup_type=ptype,
        position=pos,
    )
    pickups.append(pickup)
    return pickup


def check_auto_collect(
    bots: dict[str, BotState], pickups: list[Pickup]
) -> list[dict]:
    """Auto-collect pickups when bots move within collect_radius."""
    radius_sq = settings.pickups.collect_radius ** 2
    collected: list[dict] = []
    to_remove: list[int] = []

    for i, pickup in enumerate(pickups):
        for bot_id, bot in bots.items():
            if not bot.is_alive:
                continue
            dx = bot.position[0] - pickup.position[0]
            dy = bot.position[1] - pickup.position[1]
            if dx * dx + dy * dy <= radius_sq:
                apply_pickup(bot, pickup)
                collected.append({
                    "bot_id": bot_id,
                    "pickup_id": pickup.pickup_id,
                    "type": pickup.pickup_type,
                })
                to_remove.append(i)
                bot.round_pickups += 1
                break

    for i in reversed(to_remove):
        pickups.pop(i)
    return collected


def collect_by_action(
    bot: BotState, item_id: str, pickups: list[Pickup]
) -> bool:
    """Collect a specific pickup by use_item action."""
    radius_sq = settings.pickups.collect_radius ** 2
    for i, pickup in enumerate(pickups):
        if pickup.pickup_id != item_id:
            continue
        dx = bot.position[0] - pickup.position[0]
        dy = bot.position[1] - pickup.position[1]
        if dx * dx + dy * dy <= radius_sq:
            apply_pickup(bot, pickup)
            pickups.pop(i)
            bot.round_pickups += 1
            return True
        return False
    return False


def apply_pickup(bot: BotState, pickup: Pickup) -> None:
    """Apply a pickup's effect to a bot."""
    cfg = settings.pickups
    if pickup.pickup_type == "health_pack":
        bot.hp = min(bot.max_hp, bot.hp + cfg.health_amount)
    elif pickup.pickup_type == "speed_boost":
        bot.active_effects.append(
            Effect(name="speed_boost", remaining_ticks=cfg.speed_boost_ticks, value=cfg.speed_boost_mult)
        )
    elif pickup.pickup_type == "damage_boost":
        bot.active_effects.append(
            Effect(name="damage_boost", remaining_ticks=cfg.damage_boost_ticks, value=cfg.damage_boost_mult)
        )
    elif pickup.pickup_type == "shield_bubble":
        bot.shield_absorb += cfg.shield_bubble_hp


def tick_effects(bots: dict[str, BotState]) -> None:
    """Decrement effect timers and remove expired effects."""
    for bot in bots.values():
        remaining: list[Effect] = []
        for eff in bot.active_effects:
            eff.remaining_ticks -= 1
            if eff.remaining_ticks > 0:
                remaining.append(eff)
        bot.active_effects = remaining
        # Remove depleted shield bubbles
        if bot.shield_absorb < 0:
            bot.shield_absorb = 0


def get_effective_speed(bot: BotState) -> float:
    """Get bot speed including active speed boost effects."""
    speed = bot.speed
    for eff in bot.active_effects:
        if eff.name == "speed_boost":
            speed *= eff.value
    return speed


def get_effective_damage_mult(bot: BotState) -> float:
    """Get bot damage multiplier including active damage boost effects."""
    mult = bot.attack_multiplier
    for eff in bot.active_effects:
        if eff.name == "damage_boost":
            mult *= eff.value
    return mult
