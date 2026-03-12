"""Arena status endpoint — powered by the live game engine."""

from fastapi import APIRouter, Depends
from sqlalchemy import desc, select
from sqlalchemy.ext.asyncio import AsyncSession

from server.config import settings
from server.db.connection import get_db
from server.db.models import Bot, BotStats

from .schemas import ArenaStatusResponse

router = APIRouter(prefix="/api/v1/arena", tags=["arena"])

# Module-level engine reference set by main.py
_engine = None


def set_engine(engine: object) -> None:
    """Set the game engine reference."""
    global _engine
    _engine = engine


@router.get("/status", response_model=ArenaStatusResponse)
async def get_arena_status(
    db: AsyncSession = Depends(get_db),
) -> ArenaStatusResponse:
    """Return the current arena status from the live game engine."""
    # Find the top-rated bot by ELO
    stmt = (
        select(Bot.name)
        .join(BotStats, Bot.id == BotStats.bot_id)
        .order_by(desc(BotStats.elo))
        .limit(1)
    )
    result = await db.execute(stmt)
    top_bot_name: str | None = result.scalar_one_or_none()

    if _engine is not None:
        status = _engine.get_arena_status()
        return ArenaStatusResponse(
            status=status["status"],
            bots_connected=status["bots_connected"],
            bots_alive=status["bots_alive"],
            round_number=status["round_number"],
            round_time_remaining=settings.combat.round_duration,
            safe_zone_radius=status["safe_zone_radius"],
            top_bot=top_bot_name,
        )

    return ArenaStatusResponse(
        status="stopped",
        bots_connected=0,
        bots_alive=0,
        round_number=0,
        round_time_remaining=settings.combat.round_duration,
        safe_zone_radius=settings.arena_zone.initial_radius,
        top_bot=top_bot_name,
    )
