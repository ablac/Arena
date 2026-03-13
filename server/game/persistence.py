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
    """Update a single bot's stats in the database."""
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

    stats.kills += bot.round_kills
    stats.deaths += bot.round_deaths
    stats.damage_dealt += int(bot.round_damage_dealt)
    stats.damage_taken += int(bot.round_damage_taken)
    stats.distance_traveled += bot.round_distance
    stats.pickups_collected += bot.round_pickups
    if bot.kill_streak > stats.best_streak:
        stats.best_streak = bot.kill_streak
    stats.current_streak = bot.kill_streak
    stats.elo = bot.elo

    session.add(stats)
