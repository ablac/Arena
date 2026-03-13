"""Round management — start, end, awards, intermission."""

from __future__ import annotations

import logging
from typing import Any, TYPE_CHECKING

from server.config import settings

if TYPE_CHECKING:
    from server.game.state import BotState, RoundState

logger = logging.getLogger(__name__)


def should_end_round(
    round_state: RoundState, bots: dict[str, BotState], tick_count: int, tick_rate: int
) -> bool:
    """Check if the current round should end."""
    if not round_state.is_active:
        return False
    # Time elapsed
    elapsed_secs = (tick_count - round_state.start_tick) / tick_rate
    if elapsed_secs >= settings.combat.round_duration:
        return True
    # All bots disconnected — end immediately
    if not bots:
        return True
    # Only 1 or 0 bots alive
    alive = sum(1 for b in bots.values() if b.is_alive)
    if alive <= 1 and len(bots) >= 2:
        return True
    # Only 1 bot connected (others disconnected mid-round)
    if len(bots) <= 1:
        return True
    return False


def calculate_awards(bots: dict[str, BotState]) -> dict[str, dict[str, Any]]:
    """Calculate end-of-round awards.

    Awards: MVP, Reaper, Unkillable, Speed Demon, Sharpshooter, Berserker.
    """
    awards: dict[str, dict[str, Any]] = {}
    if not bots:
        return awards

    bot_list = list(bots.values())

    # MVP: most kills
    mvp = max(bot_list, key=lambda b: b.round_kills, default=None)
    if mvp and mvp.round_kills > 0:
        awards["MVP"] = {"bot": mvp.name, "kills": mvp.round_kills}

    # Reaper: highest K/D (min 3 kills)
    eligible = [b for b in bot_list if b.round_kills >= 3]
    if eligible:
        reaper = max(eligible, key=lambda b: b.round_kills / max(1, b.round_deaths))
        kd = reaper.round_kills / max(1, reaper.round_deaths)
        awards["Reaper"] = {"bot": reaper.name, "kd": round(kd, 2)}

    # Unkillable: longest single life
    unkillable = max(bot_list, key=lambda b: b.round_longest_life, default=None)
    if unkillable and unkillable.round_longest_life > 0:
        awards["Unkillable"] = {"bot": unkillable.name, "ticks": unkillable.round_longest_life}

    # Speed Demon: most distance traveled
    speedy = max(bot_list, key=lambda b: b.round_distance, default=None)
    if speedy and speedy.round_distance > 0:
        awards["Speed Demon"] = {"bot": speedy.name, "distance": round(speedy.round_distance, 1)}

    # Sharpshooter: highest hit rate (ranged weapons only)
    ranged = [b for b in bot_list if b.weapon in ("bow", "staff") and b.round_shots_fired >= 3]
    if ranged:
        sharp = max(ranged, key=lambda b: b.round_shots_hit / max(1, b.round_shots_fired))
        rate = sharp.round_shots_hit / max(1, sharp.round_shots_fired)
        awards["Sharpshooter"] = {"bot": sharp.name, "hit_rate": round(rate * 100, 1)}

    # Berserker: most damage dealt
    berserker = max(bot_list, key=lambda b: b.round_damage_dealt, default=None)
    if berserker and berserker.round_damage_dealt > 0:
        awards["Berserker"] = {"bot": berserker.name, "damage": round(berserker.round_damage_dealt)}

    return awards


def get_round_winner(bots: dict[str, BotState]) -> str | None:
    """Get the last bot alive, or the bot with most kills."""
    alive = [b for b in bots.values() if b.is_alive]
    if len(alive) == 1:
        return alive[0].name
    # Fall back to most kills
    if bots:
        best = max(bots.values(), key=lambda b: b.round_kills)
        if best.round_kills > 0:
            return best.name
    return None


def reset_round_stats(bots: dict[str, BotState]) -> None:
    """Reset per-round stats for all bots."""
    for bot in bots.values():
        bot.round_kills = 0
        bot.round_deaths = 0
        bot.round_damage_dealt = 0.0
        bot.round_damage_taken = 0.0
        bot.round_distance = 0.0
        bot.round_shots_fired = 0
        bot.round_shots_hit = 0
        bot.round_longest_life = 0
        bot.round_life_start_tick = 0
        bot.round_pickups = 0
        # Reset persistence snapshots so deltas start fresh next round
        bot._persisted_kills = 0
        bot._persisted_deaths = 0
        bot._persisted_damage_dealt = 0.0
        bot._persisted_damage_taken = 0.0
        bot._persisted_distance = 0.0
        bot._persisted_pickups = 0
