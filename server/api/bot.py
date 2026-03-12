"""Bot configuration and statistics endpoints."""

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from server.db.connection import get_db
from server.db.models import Bot, BotStats
from server.security.auth import api_key_dependency
from server.security.input_validator import sanitize_bot_name, validate_color, validate_stats
from server.security.rate_limiter import rate_limit_api_calls, rate_limit_bot_config

from .schemas import BotConfigRequest, BotConfigResponse, BotStatsResponse

router = APIRouter(prefix="/api/v1/bot", tags=["bot"])


@router.put("/config", response_model=BotConfigResponse)
async def update_config(
    config: BotConfigRequest,
    bot: Bot = Depends(api_key_dependency),
    db: AsyncSession = Depends(get_db),
    _rl: None = Depends(rate_limit_bot_config),
) -> BotConfigResponse:
    """Update the authenticated bot's configuration."""
    if config.name is not None:
        bot.name = sanitize_bot_name(config.name)

    if config.avatar_color is not None:
        if not validate_color(config.avatar_color):
            raise HTTPException(status_code=422, detail="Invalid avatar color format.")
        bot.avatar_color = config.avatar_color

    if config.default_loadout is not None:
        loadout = config.default_loadout
        if not validate_stats(loadout.stats.model_dump()):
            raise HTTPException(
                status_code=422,
                detail="Invalid stats allocation. Total must be 20.",
            )
        bot.default_weapon = loadout.weapon.value
        bot.default_stats = loadout.stats.model_dump()
        bot.default_fallback = loadout.fallback_behavior.value

    await db.commit()
    await db.refresh(bot)

    return BotConfigResponse(
        name=bot.name,
        avatar_color=bot.avatar_color,
        default_weapon=bot.default_weapon,
        default_stats=bot.default_stats,
        default_fallback=bot.default_fallback,
        updated_at=bot.updated_at,
    )


@router.get("/stats", response_model=BotStatsResponse)
async def get_stats(
    bot: Bot = Depends(api_key_dependency),
    db: AsyncSession = Depends(get_db),
    _rl: None = Depends(rate_limit_api_calls),
) -> BotStatsResponse:
    """Return lifetime statistics for the authenticated bot."""
    stats = await db.get(BotStats, bot.id)
    if stats is None:
        raise HTTPException(status_code=404, detail="Stats not found for this bot.")

    kd_ratio = round(
        stats.kills / stats.deaths if stats.deaths > 0 else float(stats.kills),
        2,
    )

    rank_query = (
        select(func.count())
        .select_from(BotStats)
        .where(BotStats.elo > stats.elo)
    )
    result = await db.execute(rank_query)
    rank = result.scalar_one() + 1

    # Commit to persist last_seen update from auth dependency.
    await db.commit()

    return BotStatsResponse(
        kills=stats.kills,
        deaths=stats.deaths,
        kd_ratio=kd_ratio,
        assists=stats.assists,
        damage_dealt=stats.damage_dealt,
        damage_taken=stats.damage_taken,
        current_streak=stats.current_streak,
        best_streak=stats.best_streak,
        elo=stats.elo,
        rank=rank,
        time_alive_seconds=stats.time_alive_seconds,
        longest_life_secs=stats.longest_life_secs,
        rounds_played=stats.rounds_played,
        round_wins=stats.round_wins,
        pickups_collected=stats.pickups_collected,
        distance_traveled=stats.distance_traveled,
    )
