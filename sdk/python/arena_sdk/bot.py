"""ArenaBot - base class for AI Battle Arena bots."""

from __future__ import annotations

import asyncio
import heapq
import json
import logging
import urllib.request
from typing import Any

import websockets

from . import helpers

logger = logging.getLogger("arena_sdk")


class ArenaBot:
    """Base class for arena bots. Subclass and override on_tick()."""

    def __init__(self, api_key: str, server_url: str = "wss://angel-serv.com/ws/bot",
                 stat_budget: int = 20):
        """Initialize bot with API key."""
        self.api_key = api_key
        self.server_url = server_url
        self._stat_budget = stat_budget
        self._weapon = "sword"
        self._stats: dict[str, int] = {"hp": 5, "speed": 5, "attack": 5, "defense": 5}
        self._fallback = "aggressive"
        self._ws: Any = None
        self._bot_id: str | None = None
        self._running = False
        self._tick_number = 0
        self._last_pos: list[int] = [0, 0]
        self._last_action_result: dict | None = None
        self._team = 0  # team number in team modes (0 = FFA / unassigned)

        # Terrain cache (populated by map_init)
        self._terrain: list[list[str]] | None = None
        self._map_width: int = 0
        self._map_height: int = 0
        self._cell_size: int = 1

    def set_loadout(self, weapon: str, stats: dict[str, int], fallback: str = "aggressive",
                    stat_budget: int | None = None) -> None:
        """Set loadout to use on connect. Stats must total stat_budget (default 20)."""
        budget = stat_budget if stat_budget is not None else self._stat_budget
        total = sum(stats.values())
        if total != budget:
            raise ValueError(f"Stats must total {budget}, got {total}")
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

        # Fetch terrain via REST API (pre-generated during intermission)
        self.fetch_map()

    async def on_tick(self, state: dict, nearby: list, safe_zone: dict) -> dict:
        """Override this! Called every tick. Return an action dict."""
        raise NotImplementedError("Implement on_tick() in your bot!")

    def fetch_map(self) -> bool:
        """Fetch the current terrain via REST API (GET /api/v1/arena/map).

        The server pre-generates the next round's map during intermission,
        so this can be called before the round starts. Returns True if
        terrain was loaded, False if no map is available yet.
        """
        # Derive REST base from WebSocket URL
        base = self.server_url.replace("wss://", "https://").replace("ws://", "http://")
        base = base.split("/ws/")[0]  # strip /ws/bot path
        url = f"{base}/api/v1/arena/map"
        try:
            req = urllib.request.Request(url)
            with urllib.request.urlopen(req, timeout=5) as resp:
                data = json.loads(resp.read())
            if data.get("status") == "ok":
                terrain = data.get("terrain", [])
                self._cell_size = data.get("cell_size", 1)
                width = data.get("width", 0)
                height = data.get("height", 0)
                # Normalise compact row-string format to 2D char array
                if terrain and isinstance(terrain[0], str):
                    terrain = [list(row) for row in terrain]
                self._terrain = terrain
                self._map_width = width
                self._map_height = height
                logger.info("Map loaded via REST: %dx%d", width, height)
                return True
            else:
                logger.info("No map available yet (between rounds)")
                return False
        except Exception:
            logger.warning("Failed to fetch map via REST, will retry at round_start")
            return False

    async def on_map_init(self, terrain: list, width: int, height: int) -> None:
        """Called when terrain is loaded (via REST API or legacy map_init).

        Terrain may be compact (list of row strings) or legacy (list of lists
        of single-char strings). Both are normalised to ``list[list[str]]``.

        Default implementation stores the terrain for use by helpers like
        ``get_local_map()`` and ``find_path()``. Override to add custom
        pre-processing (call ``super().on_map_init(...)`` to keep caching).

        Note: The server no longer sends map_init over WebSocket. The SDK
        fetches terrain via GET /api/v1/arena/map at connect and on round_start.
        """
        # Normalise compact row-string format to 2D char array
        if terrain and isinstance(terrain[0], str):
            terrain = [list(row) for row in terrain]
        self._terrain = terrain
        self._map_width = width
        self._map_height = height

    async def on_death(self, death_info: dict) -> None:
        """Called when bot dies. Override to customize."""

    async def on_respawn(self, respawn_info: dict) -> None:
        """Called on respawn. Override to customize."""

    async def on_round_end(self, round_info: dict) -> None:
        """Called at end of round. Override to customize."""

    # -- Action helpers --

    def move_toward(self, my_pos: list | tuple, target_pos: list | tuple) -> dict:
        """Returns a move action toward target_pos (grid direction -1/0/1)."""
        d = helpers.direction_toward(my_pos, target_pos)
        return {"action": "move", "direction": [d["x"], d["y"]]}

    def move_away(self, my_pos: list | tuple, threat_pos: list | tuple) -> dict:
        """Returns a move action away from threat_pos (grid direction -1/0/1)."""
        d = helpers.direction_away(my_pos, threat_pos)
        return {"action": "move", "direction": [d["x"], d["y"]]}

    def move_to(self, target_pos: list | tuple) -> dict:
        """Returns a move_to action toward an absolute grid position [col, row]."""
        return {"action": "move_to", "target_position": [target_pos[0], target_pos[1]]}

    def attack(self, target_id: str, target_position: tuple | list | None = None) -> dict:
        """Returns an attack action targeting target_id.

        For staff weapons, pass target_position=[col, row] for the area attack location.
        """
        action: dict = {"action": "attack", "target": target_id}
        if target_position is not None:
            action["direction"] = [target_position[0], target_position[1]]
        return action

    def staff_attack(self, target_position: tuple | list) -> dict:
        """Returns a staff area attack at the given position [col, row]."""
        return {"action": "attack", "direction": [target_position[0], target_position[1]]}

    def dodge(self, direction: dict | tuple | list) -> dict:
        """Returns a dodge action in the given direction."""
        if isinstance(direction, dict):
            return {"action": "dodge", "direction": [direction["x"], direction["y"]]}
        return {"action": "dodge", "direction": [direction[0], direction[1]]}

    def shove(self, target_id: str) -> dict:
        """Returns a shove action that knocks target back with a short stun."""
        return {"action": "shove", "target": target_id}

    def use_item(self, item_id: str) -> dict:
        """Returns a use_item action."""
        return {"action": "use_item", "item_id": item_id}

    def idle(self) -> dict:
        """Returns an idle action."""
        return {"action": "idle"}

    def place_mine(self) -> dict:
        """Place a landmine at your current position (max 3 active mines)."""
        return {"action": "place_mine"}

    def use_gravity_well(self, target_position: tuple | list) -> dict:
        """Deploy a gravity well at target position (requires gravity_well pickup charge)."""
        return {"action": "use_gravity_well", "target_position": [target_position[0], target_position[1]]}

    def grapple(self, target_id: str) -> dict:
        """Grapple a target bot (universal ability: 2 charges per round,
        12-tile range; pulls you together, damages and briefly stuns the target)."""
        return {"action": "grapple", "target": target_id}

    # -- Map / pathfinding helpers --

    def get_local_map(self, state: dict, nearby: list, radius: int = 5) -> list[str]:
        """Return an ASCII grid showing the area around the bot.

        Characters:
            @ = self
            B = other bot
            P = pickup
            terrain chars (., #, ~, V, etc.) from the cached map

        Returns a list of strings, one per row, of size ``(2*radius+1)`` square.
        If terrain is not cached yet, unknown cells show as ``?``.
        """
        pos = state.get("position", self._last_pos)
        cx, cy = int(pos[0]), int(pos[1])  # col, row

        size = 2 * radius + 1
        grid = [["?" for _ in range(size)] for _ in range(size)]

        # Fill terrain
        for dr in range(-radius, radius + 1):
            for dc in range(-radius, radius + 1):
                r, c = cy + dr, cx + dc
                gr, gc = dr + radius, dc + radius  # grid indices
                if self._terrain and 0 <= r < self._map_height and 0 <= c < self._map_width:
                    grid[gr][gc] = self._terrain[r][c]
                elif self._terrain:
                    grid[gr][gc] = "V"  # out of bounds = void

        # Place entities
        for entity in nearby:
            ep = entity.get("position")
            if ep is None:
                continue
            ec, er = int(ep[0]), int(ep[1])
            gc, gr = ec - cx + radius, er - cy + radius
            if 0 <= gc < size and 0 <= gr < size:
                etype = entity.get("type", "")
                if etype == "bot":
                    grid[gr][gc] = "B"
                elif etype == "pickup":
                    grid[gr][gc] = "P"

        # Place self
        grid[radius][radius] = "@"

        return ["".join(row) for row in grid]

    def find_path(self, start: list | tuple, goal: list | tuple) -> list[list[int]]:
        """A* pathfinding on the cached terrain grid.

        Parameters:
            start: [col, row] starting position
            goal:  [col, row] target position

        Returns:
            List of [col, row] waypoints from start to goal (inclusive of goal,
            exclusive of start). Returns empty list if no path found or terrain
            not cached.

        Walls (``#``) and void (``V``) are impassable. All other terrain is passable.
        Uses Chebyshev distance as the heuristic (diagonal moves cost 1).
        """
        if self._terrain is None:
            return []

        sc, sr = int(start[0]), int(start[1])
        gc, gr = int(goal[0]), int(goal[1])

        if not (0 <= gr < self._map_height and 0 <= gc < self._map_width):
            return []

        impassable = {"#", "V"}

        # Check goal is passable
        if self._terrain[gr][gc] in impassable:
            return []

        # A* with Chebyshev heuristic
        # Node: (col, row)
        def h(c: int, r: int) -> int:
            return max(abs(c - gc), abs(r - gr))

        # priority queue entries: (f, counter, col, row)
        counter = 0
        open_set: list[tuple[int, int, int, int]] = [(h(sc, sr), counter, sc, sr)]
        came_from: dict[tuple[int, int], tuple[int, int] | None] = {(sc, sr): None}
        g_score: dict[tuple[int, int], int] = {(sc, sr): 0}

        directions = [
            (-1, -1), (0, -1), (1, -1),
            (-1, 0),           (1, 0),
            (-1, 1),  (0, 1),  (1, 1),
        ]

        while open_set:
            _, _, cc, cr = heapq.heappop(open_set)

            if cc == gc and cr == gr:
                # Reconstruct path (exclude start)
                path: list[list[int]] = []
                node: tuple[int, int] | None = (gc, gr)
                while node is not None and node != (sc, sr):
                    path.append([node[0], node[1]])
                    node = came_from.get(node)
                path.reverse()
                return path

            current_g = g_score.get((cc, cr), 0)

            for dc, dr in directions:
                nc, nr = cc + dc, cr + dr
                if not (0 <= nr < self._map_height and 0 <= nc < self._map_width):
                    continue
                if self._terrain[nr][nc] in impassable:
                    continue
                new_g = current_g + 1
                if new_g < g_score.get((nc, nr), float("inf")):
                    g_score[(nc, nr)] = new_g
                    f = new_g + h(nc, nr)
                    counter += 1
                    heapq.heappush(open_set, (f, counter, nc, nr))
                    came_from[(nc, nr)] = (cc, cr)

        return []  # No path found

    # -- Entity helpers --

    def closest_enemy(self, nearby: list) -> dict | None:
        """Returns nearest bot entity from nearby list (excludes self)."""
        bots = [
            e for e in helpers.filter_by_type(nearby, "bot")
            if e.get("id", e.get("bot_id")) != self._bot_id
        ]
        if not bots or not self._bot_id:
            return None
        return helpers.closest_entity(self._last_pos, bots)

    def lowest_hp_enemy(self, nearby: list) -> dict | None:
        """Returns lowest HP bot entity (excludes self)."""
        bots = [
            e for e in helpers.filter_by_type(nearby, "bot")
            if e.get("id", e.get("bot_id")) != self._bot_id
        ]
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
            if msg_type == "map_init":
                # Legacy: server no longer sends this, but handle if it does
                terrain = msg.get("terrain", [])
                width = msg.get("width", 0)
                height = msg.get("height", 0)
                self._cell_size = msg.get("cell_size", 1)
                try:
                    await self.on_map_init(terrain, width, height)
                except Exception:
                    logger.exception("on_map_init error")
            elif msg_type == "round_start":
                # Fetch fresh terrain via REST API
                if self.fetch_map():
                    try:
                        await self.on_map_init(self._terrain, self._map_width, self._map_height)
                    except Exception:
                        logger.exception("on_map_init error after REST fetch")
            elif msg_type == "tick":
                self._tick_number = msg.get("tick_number", msg.get("tick", 0))
                state = msg.get("your_state", {})
                self._last_pos = state.get("position", [0, 0])
                self._last_action_result = state.get("last_action_result")
                # Team number in team-based game modes (0 = no team / FFA).
                self._team = state.get("team", 0)
                nearby = msg.get("nearby_entities", [])
                safe_zone = {
                    "center": state.get("zone_center", [0, 0]),
                    "radius": state.get("zone_radius", 100),
                    "in_safe_zone": state.get("in_safe_zone", True),
                    "distance_to_edge": state.get("distance_to_zone_edge", 0),
                    "fog_radius": state.get("fog_radius", 0),
                }
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
                self._last_pos = msg.get("position", [0, 0])
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
