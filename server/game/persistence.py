"""Batch stat persistence to PostgreSQL."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from server.db.connection import async_session_factory
from server.db.models import BotStats

if TYPE_CHECKING:
    from server.game.state import BotState

logger = logging.getLogger(__name__)


async def persist_bot_stats(bots: dict[str, BotState]) -> None:
    """Batch write bot stats to the database.

    Called periodically (every 30s), on disconnect, and at round end.
    """
    if not bots:
        return

    try:
        async with async_session_factory() as session:
            for bot in bots.values():
                await _update_bot_stats(session, bot)
            await session.commit()
    except Exception as exc:
        logger.error("Failed to persist stats: %s", exc)


async def persist_single_bot(bot: BotState) -> None:
    """Persist stats for a single bot (on disconnect)."""
    try:
        async with async_session_factory() as session:
            await _update_bot_stats(session, bot)
            await session.commit()
    except Exception as exc:
        logger.error("Failed to persist stats for %s: %s", bot.name, exc)


async def _update_bot_stats(session: AsyncSession, bot: BotState) -> None:
    """Update a single bot's stats in the database.

    Uses ``_persisted_*`` snapshot fields on BotState to compute deltas,
    preventing double-counting when called multiple times per round.
    """
    try:
        import uuid
        bot_uuid = uuid.UUID(bot.bot_id)
    except (ValueError, AttributeError):
        return

    stmt = select(BotStats).where(BotStats.bot_id == bot_uuid)
    result = await session.execute(stmt)
    stats = result.scalar_one_or_none()

    if stats is None:
        return

    # Compute deltas since last persist to avoid double-counting
    delta_kills = bot.round_kills - bot._persisted_kills
    delta_deaths = bot.round_deaths - bot._persisted_deaths
    delta_damage_dealt = int(bot.round_damage_dealt) - int(bot._persisted_damage_dealt)
    delta_damage_taken = int(bot.round_damage_taken) - int(bot._persisted_damage_taken)
    delta_distance = bot.round_distance - bot._persisted_distance
    delta_pickups = bot.round_pickups - bot._persisted_pickups

    stats.kills += delta_kills
    stats.deaths += delta_deaths
    stats.damage_dealt += delta_damage_dealt
    stats.damage_taken += delta_damage_taken
    stats.distance_traveled += delta_distance
    stats.pickups_collected += delta_pickups
    if bot.kill_streak > stats.best_streak:
        stats.best_streak = bot.kill_streak
    stats.current_streak = bot.kill_streak
    stats.elo = bot.elo

    # Update snapshot so next persist only adds new deltas
    bot._persisted_kills = bot.round_kills
    bot._persisted_deaths = bot.round_deaths
    bot._persisted_damage_dealt = bot.round_damage_dealt
    bot._persisted_damage_taken = bot.round_damage_taken
    bot._persisted_distance = bot.round_distance
    bot._persisted_pickups = bot.round_pickups

    session.add(stats)
