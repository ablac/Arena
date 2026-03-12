"""Leaderboard endpoint for ranking bots."""

from fastapi import APIRouter, HTTPException, Query
from sqlalchemy import desc, func, select
from sqlalchemy.ext.asyncio import AsyncSession

from fastapi import Depends

from server.db.connection import get_db
from server.db.models import Bot, BotStats

from .schemas import LeaderboardEntry, LeaderboardResponse

router = APIRouter(prefix="/api/v1/leaderboard", tags=["leaderboard"])

_SQL_SORT_FIELDS = {
    "kills": desc(BotStats.kills),
    "elo": desc(BotStats.elo),
    "streak": desc(BotStats.best_streak),
}


def _compute_kd(kills: int, deaths: int) -> float:
    """Compute kill/death ratio rounded to two decimals."""
    return round(kills / deaths if deaths > 0 else float(kills), 2)


@router.get("/", response_model=LeaderboardResponse)
async def get_leaderboard(
    sort: str = Query(default="elo", pattern="^(kills|kd_ratio|elo|streak)$"),
    limit: int = Query(default=50, ge=1, le=100),
    offset: int = Query(default=0, ge=0),
    db: AsyncSession = Depends(get_db),
) -> LeaderboardResponse:
    """Return a paginated leaderboard of qualifying bots."""
    # Count total qualifying bots
    count_query = (
        select(func.count())
        .select_from(BotStats)
        .where(BotStats.kills >= 10)
    )
    total_bots = (await db.execute(count_query)).scalar_one()

    if sort == "kd_ratio":
        # Fetch all qualifying rows and sort in Python
        stmt = (
            select(Bot.name, BotStats.kills, BotStats.deaths,
                   BotStats.elo, BotStats.best_streak)
            .join(BotStats, Bot.id == BotStats.bot_id)
            .where(BotStats.kills >= 10)
        )
        rows = (await db.execute(stmt)).all()

        sorted_rows = sorted(
            rows,
            key=lambda r: _compute_kd(r.kills, r.deaths),
            reverse=True,
        )
        page = sorted_rows[offset : offset + limit]

        entries = [
            LeaderboardEntry(
                rank=offset + idx + 1,
                name=row.name,
                kills=row.kills,
                deaths=row.deaths,
                kd_ratio=_compute_kd(row.kills, row.deaths),
                elo=row.elo,
                best_streak=row.best_streak,
            )
            for idx, row in enumerate(page)
        ]
    else:
        order_clause = _SQL_SORT_FIELDS[sort]
        stmt = (
            select(Bot.name, BotStats.kills, BotStats.deaths,
                   BotStats.elo, BotStats.best_streak)
            .join(BotStats, Bot.id == BotStats.bot_id)
            .where(BotStats.kills >= 10)
            .order_by(order_clause)
            .offset(offset)
            .limit(limit)
        )
        rows = (await db.execute(stmt)).all()

        entries = [
            LeaderboardEntry(
                rank=offset + idx + 1,
                name=row.name,
                kills=row.kills,
                deaths=row.deaths,
                kd_ratio=_compute_kd(row.kills, row.deaths),
                elo=row.elo,
                best_streak=row.best_streak,
            )
            for idx, row in enumerate(rows)
        ]

    return LeaderboardResponse(
        entries=entries,
        total_bots=total_bots,
        arena_status="active",
    )
