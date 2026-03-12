"""ArenaBot - base class for AI Battle Arena bots."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

import websockets

from . import helpers

logger = logging.getLogger("arena_sdk")


class ArenaBot:
    """Base class for arena bots. Subclass and override on_tick()."""

    def __init__(self, api_key: str, server_url: str = "wss://angel-serv.com/ws/bot"):
        """Initialize bot with API key."""
        self.api_key = api_key
        self.server_url = server_url
        self._weapon = "sword"
        self._stats: dict[str, int] = {"hp": 5, "speed": 5, "attack": 5, "defense": 5}
        self._fallback = "aggressive"
        self._ws: Any = None
        self._bot_id: str | None = None
        self._running = False
        self._tick_number = 0
        self._last_pos: dict | tuple = {"x": 0, "y": 0}

    def set_loadout(self, weapon: str, stats: dict[str, int], fallback: str = "aggressive") -> None:
        """Set loadout to use on connect. Stats must total 20."""
        total = sum(stats.values())
        if total != 20:
            raise ValueError(f"Stats must total 20, got {total}")
        self._weapon = weapon
        self._stats = stats
        self._fallback = fallback

    async def connect(self) -> None:
        """Connect to arena via WebSocket."""
        url = f"{self.server_url}?key={self.api_key}"
        self._ws = await websockets.connect(url)
        # Wait for connected message
        raw = await self._ws.recv()
        msg = json.loads(raw)
        if msg.get("type") != "connected":
            raise ConnectionError(f"Expected 'connected', got '{msg.get('type')}'")
        self._bot_id = msg.get("bot_id")
        logger.info("Connected as bot %s", self._bot_id)
        # Send loadout
        await self._ws.send(json.dumps({
            "type": "select_loadout",
            "weapon": self._weapon,
            "stats": self._stats,
            "fallback_behavior": self._fallback,
        }))
        # Wait for confirmation
        raw = await self._ws.recv()
        msg = json.loads(raw)
        if msg.get("type") != "loadout_confirmed":
            logger.warning("Expected 'loadout_confirmed', got '%s'", msg.get("type"))

    async def on_tick(self, state: dict, nearby: list, safe_zone: dict) -> dict:
        """Override this! Called every tick. Return an action dict."""
        raise NotImplementedError("Implement on_tick() in your bot!")

    async def on_death(self, death_info: dict) -> None:
        """Called when bot dies. Override to customize."""

    async def on_respawn(self, respawn_info: dict) -> None:
        """Called on respawn. Override to customize."""

    async def on_round_end(self, round_info: dict) -> None:
        """Called at end of round. Override to customize."""

    # -- Action helpers --

    def move_toward(self, my_pos: dict | tuple, target_pos: dict | tuple) -> dict:
        """Returns a move action toward target_pos."""
        d = helpers.direction_toward(my_pos, target_pos)
        return {"action": "move", "direction": [d["x"], d["y"]]}

    def move_away(self, my_pos: dict | tuple, threat_pos: dict | tuple) -> dict:
        """Returns a move action away from threat_pos."""
        d = helpers.direction_away(my_pos, threat_pos)
        return {"action": "move", "direction": [d["x"], d["y"]]}

    def attack(self, target_id: str) -> dict:
        """Returns an attack action targeting target_id."""
        return {"action": "attack", "target": target_id}

    def dodge(self, direction: dict | tuple) -> dict:
        """Returns a dodge action in the given direction."""
        if isinstance(direction, dict):
            return {"action": "dodge", "direction": [direction["x"], direction["y"]]}
        return {"action": "dodge", "direction": [direction[0], direction[1]]}

    def use_item(self, item_id: str) -> dict:
        """Returns a use_item action."""
        return {"action": "use_item", "item_id": item_id}

    def idle(self) -> dict:
        """Returns an idle action."""
        return {"action": "idle"}

    # -- Entity helpers --

    def closest_enemy(self, nearby: list) -> dict | None:
        """Returns nearest bot entity from nearby list."""
        bots = helpers.filter_by_type(nearby, "bot")
        if not bots or not self._bot_id:
            return None
        return helpers.closest_entity(self._last_pos, bots)

    def lowest_hp_enemy(self, nearby: list) -> dict | None:
        """Returns lowest HP bot entity."""
        bots = helpers.filter_by_type(nearby, "bot")
        return helpers.lowest_hp_entity(bots)

    def nearby_pickups(self, nearby: list) -> list:
        """Returns pickup entities sorted by distance."""
        pickups = helpers.filter_by_type(nearby, "pickup")
        pos = self._last_pos
        return sorted(pickups, key=lambda e: helpers.distance(pos, e.get("position", e)))

    # -- Internal --

    async def _game_loop(self) -> None:
        """Main loop: receive messages, dispatch to handlers, send actions."""
        self._running = True
        while self._running and self._ws:
            try:
                raw = await self._ws.recv()
            except websockets.ConnectionClosed:
                logger.info("Connection closed")
                break
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                logger.warning("Invalid JSON received")
                continue
            msg_type = msg.get("type")
            if msg_type == "tick":
                self._tick_number = msg.get("tick_number", 0)
                state = msg.get("your_state", {})
                self._last_pos = state.get("position", {"x": 0, "y": 0})
                nearby = msg.get("nearby_entities", [])
                safe_zone = msg.get("safe_zone", {})
                try:
                    action = await self.on_tick(state, nearby, safe_zone)
                except Exception:
                    logger.exception("on_tick error")
                    action = self.idle()
                payload = {"type": "action", "tick": self._tick_number}
                payload.update(action)
                await self._ws.send(json.dumps(payload))
            elif msg_type == "death":
                await self.on_death(msg)
            elif msg_type == "respawn":
                self._last_pos = msg.get("position", {"x": 0, "y": 0})
                await self.on_respawn(msg)
            elif msg_type == "round_end":
                await self.on_round_end(msg)
            elif msg_type == "error":
                logger.error("Server error: %s", msg.get("message"))
            elif msg_type == "kick":
                logger.error("Kicked: %s", msg.get("reason"))
                self._running = False
                self._kicked = True

    async def run(self) -> None:
        """Start the bot with reconnection logic (exponential backoff)."""
        self._kicked = False
        backoff = 1.0
        while not self._kicked:
            try:
                await self.connect()
                backoff = 1.0
                await self._game_loop()
            except Exception:
                logger.exception("Disconnected")
            if self._kicked:
                break
            logger.info("Reconnecting in %.0fs...", backoff)
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, 30.0)
