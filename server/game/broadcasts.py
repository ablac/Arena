"""Broadcast helpers for sending tick updates to bots and spectators."""

from __future__ import annotations

import asyncio
import logging
from typing import TYPE_CHECKING, Any

from server.config import settings
from server.ws.protocol import DeathMessage, RespawnMessage, RoundEndMessage, TickMessage

if TYPE_CHECKING:
    from fastapi import WebSocket

    from server.game.state import BotState

logger = logging.getLogger(__name__)


async def send_tick_to_bot(
    bot: BotState,
    tick_number: int,
    nearby_entities: list[dict],
    safe_zone: dict,
    kill_feed: list[dict] | None = None,
) -> None:
    """Send a tick update to a single bot's WebSocket."""
    if bot.websocket is None or not bot.is_alive:
        return

    msg = TickMessage(
        tick_number=tick_number,
        your_state={
            "bot_id": bot.bot_id,
            "position": bot.position,
            "hp": bot.hp,
            "max_hp": bot.max_hp,
            "speed": bot.speed,
            "weapon": bot.weapon,
            "cooldown_remaining": round(bot.cooldown_remaining, 2),
            "is_alive": bot.is_alive,
            "kill_streak": bot.kill_streak,
            "dodge_cooldown": bot.dodge_cooldown,
            "invuln_ticks": bot.invuln_ticks,
            "shield_absorb": bot.shield_absorb,
            "effects": [{"name": e.name, "ticks": e.remaining_ticks} for e in bot.active_effects],
            "kill_feed": kill_feed or [],
        },
        nearby_entities=nearby_entities,
        safe_zone=safe_zone,
        view_radius=settings.game.view_radius,
    )

    try:
        await bot.websocket.send_json(msg.model_dump())
    except Exception:
        logger.debug("Failed to send tick to bot %s", bot.bot_id)


async def send_death_to_bot(bot: BotState, event: dict) -> None:
    """Send a death notification to a bot."""
    if bot.websocket is None:
        return

    msg = DeathMessage(
        killed_by=event.get("killer_name", "unknown"),
        weapon_used=event.get("weapon", "unknown"),
        damage=0,
        your_kills_this_life=event.get("kills_this_life", 0),
        respawn_in_seconds=settings.combat.respawn_time,
    )

    try:
        await bot.websocket.send_json(msg.model_dump())
    except Exception:
        logger.debug("Failed to send death msg to bot %s", bot.bot_id)


async def send_respawn_to_bot(bot: BotState, event: dict) -> None:
    """Send a respawn notification to a bot."""
    if bot.websocket is None:
        return

    msg = RespawnMessage(
        position=event["position"],
        hp=event["hp"],
    )

    try:
        await bot.websocket.send_json(msg.model_dump())
    except Exception:
        logger.debug("Failed to send respawn msg to bot %s", bot.bot_id)


async def send_round_end(
    bots: dict[str, BotState], round_number: int, winner: str | None,
    intermission_time: int,
) -> None:
    """Send round-end results to all connected bots."""
    for bot in bots.values():
        if not bot.websocket:
            continue
        msg = RoundEndMessage(
            round_number=round_number,
            your_stats={"kills": bot.round_kills, "deaths": bot.round_deaths,
                        "damage": round(bot.round_damage_dealt)},
            round_winner=winner, next_round_in=intermission_time,
        )
        try:
            await bot.websocket.send_json(msg.model_dump())
        except Exception:
            pass


async def broadcast_to_spectators(
    spectators: list[WebSocket],
    state: dict[str, Any],
) -> None:
    """Send arena state to all connected spectators. Remove broken connections."""
    disconnected: list[int] = []

    for i, ws in enumerate(spectators):
        try:
            await ws.send_json(state)
        except Exception:
            disconnected.append(i)

    # Remove disconnected spectators (reverse order to preserve indices)
    for i in reversed(disconnected):
        spectators.pop(i)
