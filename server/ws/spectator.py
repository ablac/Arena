"""WebSocket endpoint for spectator connections."""

from __future__ import annotations

import logging
from typing import Any

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from server.config import settings
from server.ws.protocol import ErrorMessage

logger = logging.getLogger(__name__)
router = APIRouter()

# Module-level reference set by main.py on startup
_engine = None


def set_engine(engine: Any) -> None:
    """Set the game engine reference for the handler."""
    global _engine
    _engine = engine


@router.websocket("/ws/watch")
async def spectator_websocket(ws: WebSocket) -> None:
    """Handle a spectator's WebSocket connection."""
    if _engine is None:
        await ws.close(code=1013, reason="Engine not ready")
        return

    # Check spectator limit
    if len(_engine.spectators) >= settings.game.max_spectators:
        await ws.accept()
        await ws.send_json(ErrorMessage(message="Spectator limit reached").model_dump())
        await ws.close(code=1013)
        return

    await ws.accept()
    _engine.spectators.append(ws)
    logger.info("Spectator connected (%d total)", len(_engine.spectators))

    # Send initial arena state
    try:
        state = _engine.get_spectator_state()
        await ws.send_json(state)
    except Exception:
        _engine.spectators.remove(ws)
        return

    # Keep connection alive — spectator receives broadcasts from engine
    try:
        while True:
            # Spectators don't send meaningful messages, but we need to
            # keep the connection open and handle pings/disconnects
            await ws.receive_text()
    except WebSocketDisconnect:
        logger.info("Spectator disconnected")
    except Exception:
        pass
    finally:
        if ws in _engine.spectators:
            _engine.spectators.remove(ws)
        logger.info("Spectators remaining: %d", len(_engine.spectators))
