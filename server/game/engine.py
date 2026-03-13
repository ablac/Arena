"""GameEngine — the heart of the AI Battle Arena."""
from __future__ import annotations

import asyncio
import logging
import math
import time
from typing import Any

from fastapi import WebSocket

from server.config import settings
from server.game.arena_map import ArenaMap
from server.game.broadcasts import (
    broadcast_to_spectators, send_death_to_bot, send_kill_to_bot,
    send_round_end, send_tick_to_bot,
)
from server.game.combat import process_combat, process_staff_impacts
from server.game.engine_helpers import (
    apply_fallbacks, handle_kill_credits,
    process_use_items, update_life_tracking,
)
from server.game.kill_feed import KillFeed
from server.game.movement import process_movement, reset_nav_grid, separate_bots
from server.game.persistence import persist_bot_stats
from server.game.pickups import check_auto_collect, maybe_spawn_pickup, tick_effects
from server.game.projectiles import update_projectiles
from server.game.rounds import get_round_winner, reset_round_stats, should_end_round
from server.game.spatial import SpatialGrid
from server.game.spawner import check_deaths, spawn_bot
from server.security.input_validator import validate_derived_stats
from server.game.state import BotState, Pickup, Projectile, RoundState, StaffImpact
from server.game.views import bot_to_nearby_dict, build_arena_status, build_spectator_state
from server.ws.protocol import KickMessage, LobbyMessage, RoundStartMessage

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
        self._last_killfeed_tick: int = 0
        self._waiting_bots: dict[str, BotState] = {}  # bots queued for next round

    async def run(self) -> None:
        """Main tick loop — runs as an asyncio background task."""
        self.running = True
        self.round.in_lobby = True
        logger.info("Game engine started at %d ticks/sec (lobby mode)", self._tick_rate)
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
        if self.round.in_lobby:
            await self._tick_lobby()
            return
        if self.round.in_intermission:
            await self._tick_intermission()
            return
        apply_fallbacks(self.bots, self.get_nearby_bots)
        process_use_items(self.bots, self.pickups)
        process_movement(self.bots, self.arena, self.grid, self.arena.obstacles)
        process_combat(self.bots, self._tick_rate, self.arena.obstacles, self.projectiles, self.staff_impacts)
        update_projectiles(self.projectiles, self.bots, self.arena.obstacles, self._tick_rate)
        process_staff_impacts(self.staff_impacts, self.bots)
        # Resync grid after knockback moved bots without grid updates
        for bid, b in self.bots.items():
            if b.is_alive:
                self.grid.update(bid, b.position[0], b.position[1])
        separate_bots(self.bots, self.arena, self.grid)
        death_events = check_deaths(self.bots, self.grid, self.tick_count)
        kill_events = handle_kill_credits(death_events, self.bots, self.kill_feed, self.tick_count)
        maybe_spawn_pickup(self.pickups, self.arena, self.tick_count)
        check_auto_collect(self.bots, self.pickups)
        tick_effects(self.bots)
        update_life_tracking(self.bots, self.tick_count)
        await self._check_afk()
        await self._send_bot_updates()
        if self.tick_count % self._spec_interval == 0:
            await self._send_spectator_update()
            # Clear last_action after spectator broadcast has consumed them
            for bot in self.bots.values():
                bot.last_action = None
                bot.last_action_target = None
            # Send lobby updates to bots waiting for next round
            if self._waiting_bots:
                await self._send_waiting_updates()
        await self._send_event_messages(death_events, send_death_to_bot)
        await self._send_event_messages(kill_events, send_kill_to_bot)
        if self.tick_count - self._last_persist_tick >= self._persist_interval:
            self._last_persist_tick = self.tick_count
            asyncio.create_task(persist_bot_stats(self.bots))
        if should_end_round(self.round, self.bots, self.tick_count, self._tick_rate):
            await self._end_round()
        for bot in self.bots.values():
            bot.pending_action = None
            bot.hits_received.clear()
            bot.last_action_result = None

    def _start_round(self) -> None:
        self.round.round_number += 1
        self.round.start_tick = self.tick_count
        self.round.is_active, self.round.in_intermission = True, False
        self.arena.reset()
        reset_nav_grid()
        for collection in (self.pickups, self.projectiles, self.staff_impacts):
            collection.clear()
        self.kill_feed.clear()
        # Audit bot stats before the round — kick any with tampered values
        self._audit_bot_stats()
        reset_round_stats(self.bots)
        for bot in self.bots.values():
            spawn_bot(bot, self.arena, self.grid)
            bot.round_life_start_tick = self.tick_count
        logger.info("Round %d started with %d bots", self.round.round_number, len(self.bots))

    async def _end_round(self) -> None:
        self.round.is_active = False
        winner = get_round_winner(self.bots)
        await send_round_end(self.bots, self.round.round_number, winner, settings.combat.intermission_time)
        await persist_bot_stats(self.bots)
        self.round.in_intermission = True
        self.round.intermission_ticks = settings.combat.intermission_time * self._tick_rate
        logger.info("Round %d ended. Winner: %s", self.round.round_number, winner)

    async def _tick_lobby(self) -> None:
        """Handle lobby state — wait for enough bots, then countdown and start."""
        # Merge any bots that were waiting during the previous round
        if self._waiting_bots:
            for bot_id, bot in self._waiting_bots.items():
                self.bots[bot_id] = bot
                logger.info("Bot %s moved from waiting queue to lobby", bot.name)
            self._waiting_bots.clear()

        alive_bots = len(self.bots)
        min_needed = settings.combat.min_bots_to_start

        if alive_bots >= min_needed:
            if self.round.lobby_countdown_ticks <= 0:
                # Start the countdown
                self.round.lobby_countdown_ticks = settings.combat.lobby_countdown * self._tick_rate
            self.round.lobby_countdown_ticks -= 1
            if self.round.lobby_countdown_ticks <= 0:
                # Countdown finished — start the round
                self.round.in_lobby = False
                self._start_round()
                await self._send_round_start()
                return
        else:
            # Not enough bots, reset countdown
            self.round.lobby_countdown_ticks = 0

        # Broadcast lobby state to bots and spectators every tick
        if self.tick_count % self._spec_interval == 0:
            await self._send_lobby_updates()

    async def _tick_intermission(self) -> None:
        self.round.intermission_ticks -= 1
        # Send lobby updates to bots waiting for next round
        if self._waiting_bots and self.tick_count % self._spec_interval == 0:
            await self._send_waiting_updates()
        if self.round.intermission_ticks <= 0:
            # Go back to lobby instead of directly starting a new round
            self.round.in_intermission = False
            self.round.in_lobby = True
            self.round.lobby_countdown_ticks = 0
            logger.info("Intermission ended, returning to lobby")

    def _audit_bot_stats(self) -> None:
        """Verify all bots' derived stats match their raw allocations.

        Kicks any bot whose hp/speed/attack/defense values have drifted
        from what ``compute_stats(bot.stats)`` would produce.
        """
        to_kick: list[tuple[str, str]] = []
        for bot_id, bot in self.bots.items():
            err = validate_derived_stats(bot)
            if err:
                to_kick.append((bot_id, err))
        for bot_id, reason in to_kick:
            bot = self.bots.get(bot_id)
            if bot:
                logger.warning("Kicking bot %s — stat integrity violation: %s", bot.name, reason)
                if bot.websocket:
                    try:
                        import asyncio as _aio
                        _aio.get_event_loop().create_task(
                            bot.websocket.send_json(
                                KickMessage(reason=f"Stat violation: {reason}").model_dump()
                            )
                        )
                    except Exception:
                        pass
            self.remove_bot(bot_id)

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
        kills = self.kill_feed.get_recent(5)
        cx, cy = self.arena.center_x, self.arena.center_y
        zone_r = self.arena.safe_zone_radius
        for bot in self.bots.values():
            if not bot.is_alive or bot.websocket is None:
                continue
            dx = bot.position[0] - cx
            dy = bot.position[1] - cy
            dist = math.sqrt(dx * dx + dy * dy)
            zone_info = {
                "in_safe_zone": dist <= zone_r,
                "distance_to_zone_edge": round(zone_r - dist, 1),
                "zone_radius": round(zone_r, 1),
                "zone_center": (cx, cy),
            }
            nearby = [bot_to_nearby_dict(b) for b in self.get_nearby_bots(bot)]
            await send_tick_to_bot(bot, self.tick_count, nearby, kills, zone_info=zone_info)

    async def _send_spectator_update(self) -> None:
        new_kills = self.kill_feed.get_since(self._last_killfeed_tick)
        self._last_killfeed_tick = self.tick_count
        state = build_spectator_state(
            self.tick_count, self.bots, self.pickups,
            new_kills, self.arena.get_obstacles_dicts(),
        )
        await broadcast_to_spectators(self.spectators, state)

    async def _send_event_messages(self, events: list[dict], send_fn) -> None:
        for event in events:
            bot = self.bots.get(event["bot_id"])
            if bot:
                await send_fn(bot, event)

    async def _send_lobby_updates(self) -> None:
        """Send lobby status to all bots and spectators."""
        total_bots = len(self.bots)
        min_needed = settings.combat.min_bots_to_start
        countdown = None
        if self.round.lobby_countdown_ticks > 0:
            countdown = max(1, self.round.lobby_countdown_ticks // self._tick_rate)
        players = [
            {"name": b.name, "avatar_color": b.avatar_color, "weapon": b.weapon}
            for b in self.bots.values()
        ]
        lobby_msg = LobbyMessage(
            bots_connected=total_bots,
            bots_needed=min_needed,
            countdown=countdown,
            players=players,
        ).model_dump()

        # Send to bots
        for bot in self.bots.values():
            if bot.websocket:
                try:
                    await bot.websocket.send_json(lobby_msg)
                except Exception:
                    pass

        # Send to spectators
        spectator_state = {
            "type": "lobby_state",
            "tick": self.tick_count,
            "bots_connected": total_bots,
            "bots_needed": min_needed,
            "countdown": countdown,
            "players": players,
        }
        await broadcast_to_spectators(self.spectators, spectator_state)

    async def _send_waiting_updates(self) -> None:
        """Send lobby-style updates to bots waiting for the next round."""
        waiting_count = len(self._waiting_bots)
        active_count = len(self.bots)
        players = [
            {"name": b.name, "avatar_color": b.avatar_color, "weapon": b.weapon}
            for b in self._waiting_bots.values()
        ]
        msg = LobbyMessage(
            bots_connected=waiting_count,
            bots_needed=0,
            countdown=None,
            players=players,
        ).model_dump()
        # Override type to indicate waiting state
        msg["waiting_for_round"] = True
        msg["active_bots"] = active_count

        for bot in self._waiting_bots.values():
            if bot.websocket:
                try:
                    await bot.websocket.send_json(msg)
                except Exception:
                    pass

    async def _send_round_start(self) -> None:
        """Send round_start message to all bots."""
        obstacles = self.arena.get_obstacles_dicts()
        all_positions = {bid: b.position for bid, b in self.bots.items()}
        safe_zone = self.arena.get_zone_state()
        for bot in self.bots.values():
            if bot.websocket:
                msg = RoundStartMessage(
                    round_number=self.round.round_number,
                    position=bot.position,
                    bots_in_round=len(self.bots),
                    obstacles=obstacles,
                    all_positions=all_positions,
                    safe_zone=safe_zone,
                ).model_dump()
                try:
                    await bot.websocket.send_json(msg)
                except Exception:
                    pass

    def add_bot(self, bot: BotState) -> None:
        if self.round.in_lobby:
            self.bots[bot.bot_id] = bot
        else:
            # Round is active or in intermission — queue for next round
            self._waiting_bots[bot.bot_id] = bot
            bot.is_alive = False
            logger.info("Bot %s queued for next round (round in progress)", bot.name)
        bot.round_life_start_tick = self.tick_count

    def remove_bot(self, bot_id: str) -> None:
        bot = self.bots.pop(bot_id, None)
        if bot is None:
            bot = self._waiting_bots.pop(bot_id, None)
        if bot:
            self.grid.remove(bot_id)
            try:
                asyncio.create_task(persist_bot_stats({bot_id: bot}))
            except RuntimeError:
                pass

    def get_arena_status(self) -> dict[str, Any]:
        return build_arena_status(
            self.running, self.bots, self.round.round_number,
            self.tick_count,
        )

    def get_spectator_state(self) -> dict[str, Any]:
        return build_spectator_state(
            self.tick_count, self.bots, self.pickups,
            self.kill_feed.get_all(), self.arena.get_obstacles_dicts(),
        )
