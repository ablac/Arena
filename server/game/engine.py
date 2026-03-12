"""GameEngine — the heart of the AI Battle Arena."""
from __future__ import annotations

import asyncio
import logging
import time
from typing import Any

from fastapi import WebSocket

from server.config import settings
from server.game.arena_map import ArenaMap
from server.game.broadcasts import (
    broadcast_to_spectators, send_death_to_bot, send_respawn_to_bot,
    send_round_end, send_tick_to_bot,
)
from server.game.combat import process_combat, process_staff_impacts
from server.game.engine_helpers import (
    apply_fallbacks, apply_zone_damage, handle_kill_credits,
    process_use_items, update_life_tracking,
)
from server.game.kill_feed import KillFeed
from server.game.movement import process_movement
from server.game.persistence import persist_bot_stats
from server.game.pickups import check_auto_collect, maybe_spawn_pickup, tick_effects
from server.game.projectiles import update_projectiles
from server.game.rounds import get_round_winner, reset_round_stats, should_end_round
from server.game.spatial import SpatialGrid
from server.game.spawner import check_deaths, process_respawns, spawn_bot
from server.game.state import BotState, Pickup, Projectile, RoundState, StaffImpact
from server.game.views import bot_to_nearby_dict, build_arena_status, build_spectator_state
from server.ws.protocol import KickMessage

logger = logging.getLogger(__name__)


class GameEngine:
    """Central game engine running the arena tick loop."""

    def __init__(self) -> None:
        self.bots: dict[str, BotState] = {}
        self.pickups: list[Pickup] = []
        self.projectiles: list[Projectile] = []
        self.staff_impacts: list[StaffImpact] = []
        self.round: RoundState = RoundState()
        self.tick_count: int = 0
        self.running: bool = False
        self.arena: ArenaMap = ArenaMap()
        self.grid: SpatialGrid = SpatialGrid()
        self.spectators: list[WebSocket] = []
        self.kill_feed: KillFeed = KillFeed()
        self._tick_rate: int = settings.game.tick_rate
        self._view_radius: int = settings.game.view_radius
        self._afk_ticks: int = settings.network.afk_timeout_ticks
        self._spec_interval: int = settings.network.spectator_broadcast_interval
        self._persist_interval: int = settings.network.persist_interval_secs * self._tick_rate
        self._last_persist_tick: int = 0

    async def run(self) -> None:
        """Main tick loop — runs as an asyncio background task."""
        self.running = True
        self._start_round()
        logger.info("Game engine started at %d ticks/sec", self._tick_rate)
        tick_duration = 1.0 / self._tick_rate
        while self.running:
            start = time.monotonic()
            await self.tick()
            elapsed = time.monotonic() - start
            await asyncio.sleep(max(0.0, tick_duration - elapsed))
        logger.info("Game engine stopped")

    async def tick(self) -> None:
        """Execute one game tick."""
        self.tick_count += 1
        if self.round.in_intermission:
            self._tick_intermission()
            return
        apply_fallbacks(self.bots, self.get_nearby_bots)
        process_use_items(self.bots, self.pickups)
        process_movement(self.bots, self.arena, self.grid, self.arena.obstacles)
        process_combat(self.bots, self._tick_rate, self.arena.obstacles, self.projectiles, self.staff_impacts)
        update_projectiles(self.projectiles, self.bots, self.arena.obstacles, self._tick_rate)
        process_staff_impacts(self.staff_impacts, self.bots)
        death_events = check_deaths(self.bots, self.grid, self.tick_count)
        handle_kill_credits(death_events, self.bots, self.kill_feed, self.tick_count)
        respawn_events = process_respawns(self.bots, self.arena, self.grid, self._tick_rate)
        self.arena.update_zone(self.tick_count, self._tick_rate)
        apply_zone_damage(self.bots, self.arena)
        maybe_spawn_pickup(self.pickups, self.arena, self.tick_count)
        check_auto_collect(self.bots, self.pickups)
        tick_effects(self.bots)
        update_life_tracking(self.bots, self.tick_count)
        await self._check_afk()
        await self._send_bot_updates()
        if self.tick_count % self._spec_interval == 0:
            await self._send_spectator_update()
        await self._send_event_messages(death_events, send_death_to_bot)
        await self._send_event_messages(respawn_events, send_respawn_to_bot)
        if self.tick_count - self._last_persist_tick >= self._persist_interval:
            self._last_persist_tick = self.tick_count
            asyncio.create_task(persist_bot_stats(self.bots))
        if should_end_round(self.round, self.bots, self.tick_count, self._tick_rate):
            await self._end_round()
        for bot in self.bots.values():
            bot.pending_action = None

    def _start_round(self) -> None:
        self.round.round_number += 1
        self.round.start_tick = self.tick_count
        self.round.is_active, self.round.in_intermission = True, False
        self.arena.reset()
        for collection in (self.pickups, self.projectiles, self.staff_impacts):
            collection.clear()
        self.kill_feed.clear()
        reset_round_stats(self.bots)
        for bot in self.bots.values():
            spawn_bot(bot, self.arena, self.grid)
            bot.round_life_start_tick = self.tick_count
        logger.info("Round %d started", self.round.round_number)

    async def _end_round(self) -> None:
        self.round.is_active = False
        winner = get_round_winner(self.bots)
        await send_round_end(self.bots, self.round.round_number, winner, settings.combat.intermission_time)
        await persist_bot_stats(self.bots)
        self.round.in_intermission = True
        self.round.intermission_ticks = settings.combat.intermission_time * self._tick_rate
        logger.info("Round %d ended. Winner: %s", self.round.round_number, winner)

    def _tick_intermission(self) -> None:
        self.round.intermission_ticks -= 1
        if self.round.intermission_ticks <= 0:
            self._start_round()

    async def _check_afk(self) -> None:
        for bot_id, bot in list(self.bots.items()):
            if bot.is_alive and bot.last_action_tick > 0 and self.tick_count - bot.last_action_tick >= self._afk_ticks:
                await self._kick_bot(bot_id, "AFK: no actions received")

    async def _kick_bot(self, bot_id: str, reason: str) -> None:
        bot = self.bots.get(bot_id)
        if bot and bot.websocket:
            try:
                await bot.websocket.send_json(KickMessage(reason=reason).model_dump())
                await bot.websocket.close()
            except Exception:
                pass
        self.remove_bot(bot_id)

    def get_nearby_bots(self, bot: BotState) -> list[BotState]:
        ids = self.grid.query_radius(bot.position[0], bot.position[1], self._view_radius)
        return [self.bots[b] for b in ids if b != bot.bot_id and b in self.bots and self.bots[b].is_alive]

    async def _send_bot_updates(self) -> None:
        zone = self.arena.get_zone_state()
        kills = self.kill_feed.get_recent(5)
        for bot in self.bots.values():
            if not bot.is_alive or bot.websocket is None:
                continue
            nearby = [bot_to_nearby_dict(b) for b in self.get_nearby_bots(bot)]
            await send_tick_to_bot(bot, self.tick_count, nearby, zone, kills)

    async def _send_spectator_update(self) -> None:
        state = build_spectator_state(
            self.tick_count, self.bots, self.arena.get_zone_state(), self.pickups,
            self.kill_feed.get_all(), self.arena.get_obstacles_dicts(),
        )
        await broadcast_to_spectators(self.spectators, state)

    async def _send_event_messages(self, events: list[dict], send_fn) -> None:
        for event in events:
            bot = self.bots.get(event["bot_id"])
            if bot:
                await send_fn(bot, event)

    def add_bot(self, bot: BotState) -> None:
        self.bots[bot.bot_id] = bot
        spawn_bot(bot, self.arena, self.grid)
        bot.round_life_start_tick = self.tick_count

    def remove_bot(self, bot_id: str) -> None:
        bot = self.bots.pop(bot_id, None)
        if bot:
            self.grid.remove(bot_id)
            try:
                asyncio.create_task(persist_bot_stats({bot_id: bot}))
            except RuntimeError:
                pass

    def get_arena_status(self) -> dict[str, Any]:
        return build_arena_status(
            self.running, self.bots, self.round.round_number,
            self.tick_count, self.arena.safe_zone_radius,
        )

    def get_spectator_state(self) -> dict[str, Any]:
        return build_spectator_state(
            self.tick_count, self.bots, self.arena.get_zone_state(), self.pickups,
            self.kill_feed.get_all(), self.arena.get_obstacles_dicts(),
        )
