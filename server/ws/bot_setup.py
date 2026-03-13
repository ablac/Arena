"""Bot connection setup helpers — auth, ELO loading, loadout, stats.

Extracted from bot_handler.py to keep files under 200 lines.
"""

from __future__ import annotations

import asyncio
import logging

from fastapi import WebSocket
from sqlalchemy import select

from server.config import settings
from server.db.connection import async_session_factory
from server.db.models import Bot, BotStats
from server.game.weapons import get_available_weapons
from server.security.auth import get_bot_by_key
from server.security.input_validator import validate_stats
from server.ws.protocol import ErrorMessage, LoadoutSelectMessage, parse_bot_message

logger = logging.getLogger(__name__)


async def authenticate(key: str) -> Bot | None:
    """Validate API key and return the Bot record."""
    async with async_session_factory() as session:
        return await get_bot_by_key(session, key)


async def load_elo(bot_id: str) -> int:
    """Load a bot's ELO rating from the database."""
    try:
        import uuid
        bot_uuid = uuid.UUID(bot_id)
        async with async_session_factory() as session:
            result = await session.execute(
                select(BotStats.elo).where(BotStats.bot_id == bot_uuid)
            )
            elo = result.scalar_one_or_none()
            return elo if elo is not None else 1000
    except Exception:
        return 1000


async def wait_for_loadout(ws: WebSocket, bot: Bot) -> dict:
    """Wait for loadout selection with timeout. Falls back to defaults."""
    timeout = settings.network.loadout_timeout_secs
    defaults = {
        "weapon": bot.default_weapon,
        "stats": bot.default_stats,
        "fallback_behavior": bot.default_fallback,
    }
    try:
        raw = await asyncio.wait_for(ws.receive_json(), timeout=timeout)
        msg = parse_bot_message(raw)
        if not isinstance(msg, LoadoutSelectMessage):
            await ws.send_json(ErrorMessage(message="Expected select_loadout").model_dump())
            return defaults
        if msg.weapon not in get_available_weapons():
            await ws.send_json(ErrorMessage(message=f"Unknown weapon: {msg.weapon}").model_dump())
            return defaults
        if not validate_stats(msg.stats):
            await ws.send_json(ErrorMessage(message="Invalid stats").model_dump())
            return defaults
        return {"weapon": msg.weapon, "stats": msg.stats, "fallback_behavior": msg.fallback_behavior}
    except asyncio.TimeoutError:
        return defaults
    except Exception:
        return defaults


def compute_stats(stats: dict[str, int]) -> dict[str, float]:
    """Compute derived stats from raw stat allocation."""
    return {
        "max_hp": 100 + stats.get("hp", 5) * 10,
        "move_speed": 3 + stats.get("speed", 5) * 0.5,
        "attack_mult": 1.0 + stats.get("attack", 5) * 0.1,
        "defense_red": stats.get("defense", 5) * 0.03,
    }
