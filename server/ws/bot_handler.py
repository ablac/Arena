"""WebSocket endpoint for bot connections."""

from __future__ import annotations

import logging
import time
from typing import Any

from fastapi import APIRouter, Query, WebSocket, WebSocketDisconnect
from server.config import settings
from server.game.state import Action, ActionType, BotState
from server.game.weapons import get_available_weapons
from server.ws.bot_setup import LoadoutRejected, authenticate, compute_stats, load_elo, wait_for_loadout
from server.ws.protocol import (
    ConnectedMessage,
    ErrorMessage,
    LoadoutConfirmedMessage,
    LoadoutSelectMessage,
    parse_bot_message,
)

logger = logging.getLogger(__name__)
router = APIRouter()

# Module-level reference set by main.py on startup
_engine = None


def set_engine(engine: Any) -> None:
    """Set the game engine reference for the handler."""
    global _engine
    _engine = engine


@router.websocket("/ws/bot")
async def bot_websocket(ws: WebSocket, key: str = Query(...)) -> None:
    """Handle a bot's WebSocket connection lifecycle."""
    if _engine is None:
        await ws.close(code=1013, reason="Engine not ready")
        return

    # Authenticate via API key
    bot_record = await authenticate(key)
    if bot_record is None:
        await ws.accept()
        await ws.send_json(ErrorMessage(message="Invalid API key").model_dump())
        await ws.close(code=1008)
        return

    # Check if key already has active connection
    bot_id = str(bot_record.id)
    if bot_id in _engine.bots:
        old_bot = _engine.bots[bot_id]
        if old_bot.websocket:
            try:
                await old_bot.websocket.close(code=1008, reason="New connection")
            except Exception:
                pass
        _engine.remove_bot(bot_id)

    await ws.accept()

    # Send ConnectedMessage
    last_loadout = {
        "weapon": bot_record.default_weapon,
        "stats": bot_record.default_stats,
        "fallback_behavior": bot_record.default_fallback,
    }
    connected_msg = ConnectedMessage(
        bot_id=bot_id,
        arena_size=(settings.game.arena_width, settings.game.arena_height),
        available_weapons=get_available_weapons(),
        stat_budget=settings.combat.stat_budget,
        stat_min=settings.combat.stat_min,
        stat_max=settings.combat.stat_max,
        timeout_seconds=settings.network.loadout_timeout_secs,
        last_loadout=last_loadout,
    )
    await ws.send_json(connected_msg.model_dump())

    # Wait for loadout selection — kick on invalid stats
    try:
        loadout = await wait_for_loadout(ws, bot_record)
    except LoadoutRejected as exc:
        logger.warning("Bot %s kicked: invalid loadout — %s", bot_record.name, exc)
        await ws.send_json(ErrorMessage(message=f"Kicked: {exc}").model_dump())
        await ws.close(code=1008)
        return
    weapon = loadout["weapon"]
    stats = loadout["stats"]
    fallback = loadout["fallback_behavior"]

    # Compute derived stats
    computed = compute_stats(stats, weapon)

    # Create BotState and register with engine
    bot_elo = await load_elo(bot_id)

    bot_state = BotState(
        bot_id=bot_id,
        api_key_id=str(bot_record.api_key_id),
        name=bot_record.name,
        max_hp=int(computed["max_hp"]),
        hp=int(computed["max_hp"]),
        speed=computed["move_speed"],
        attack_multiplier=computed["attack_mult"],
        defense_reduction=computed["defense_red"],
        weapon=weapon,
        fallback_behavior=fallback,
        websocket=ws,
        avatar_color=bot_record.avatar_color,
        stats=stats,
        elo=bot_elo,
    )

    # Send loadout confirmation
    confirm_msg = LoadoutConfirmedMessage(
        weapon=weapon, stats=stats, computed=computed, position=(0.0, 0.0),
    )

    _engine.add_bot(bot_state)
    confirm_msg.position = bot_state.position
    await ws.send_json(confirm_msg.model_dump())

    # Enter message loop
    try:
        await _message_loop(ws, bot_state)
    except WebSocketDisconnect:
        logger.info("Bot %s disconnected", bot_state.name)
    except Exception as exc:
        logger.error("Bot %s error: %s", bot_state.name, exc)
    finally:
        _engine.remove_bot(bot_id)


async def _message_loop(ws: WebSocket, bot: BotState) -> None:
    """Receive and process action messages from a bot."""
    max_msgs_per_sec = settings.network.ws_max_messages_per_sec
    msg_timestamps: list[float] = []

    while True:
        raw = await ws.receive_json()

        # Rate limiting
        now = time.monotonic()
        msg_timestamps = [t for t in msg_timestamps if now - t < 1.0]
        if len(msg_timestamps) >= max_msgs_per_sec:
            logger.warning("Rate limited bot %s (%d msgs/sec)", bot.name, len(msg_timestamps))
            await ws.send_json(ErrorMessage(message="Rate limited").model_dump())
            continue
        msg_timestamps.append(now)

        msg = parse_bot_message(raw)
        if msg is None:
            await ws.send_json(ErrorMessage(message="Invalid message").model_dump())
            continue
        # Reject loadout changes during gameplay
        if isinstance(msg, LoadoutSelectMessage):
            await ws.send_json(ErrorMessage(message="Cannot change loadout mid-game").model_dump())
            continue
        # It's an ActionMessage
        action_map = {
            "move": ActionType.MOVE,
            "move_to": ActionType.MOVE_TO,
            "attack": ActionType.ATTACK,
            "dodge": ActionType.DODGE,
            "shove": ActionType.SHOVE,
            "use_item": ActionType.USE_ITEM,
            "idle": ActionType.IDLE,
        }
        action_type = action_map.get(msg.action, ActionType.IDLE)
        target_pos = None
        if action_type == ActionType.MOVE_TO and msg.target_position is not None:
            tp = msg.target_position
            # Clamp target position to arena bounds
            arena_w = settings.game.arena_width
            arena_h = settings.game.arena_height
            target_pos = (
                max(0.0, min(float(arena_w), float(tp[0]))),
                max(0.0, min(float(arena_h), float(tp[1]))),
            )
        bot.pending_action = Action(
            action_type=action_type,
            target_id=msg.target,
            direction=msg.direction,
            item_id=msg.item_id,
            target_position=target_pos,
        )
        # Staff uses target_position for area attacks
        if bot.weapon == "staff" and action_type == ActionType.ATTACK:
            bot.pending_action.target_position = msg.direction

        bot.last_action_tick = _engine.tick_count
