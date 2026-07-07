"""Hunter Bot - Target the Best.

Uses the REST leaderboard API to identify the top-ranked bot, then hunts
it down. Switches to survival mode when HP is critical and uses knockback
to push enemies out of the safe zone.

Usage: python hunter.py <api_key> [server_url]
"""

import asyncio
import sys
from urllib.parse import urlparse

import aiohttp

from arena_sdk import ArenaBot

_SURVIVAL_THRESHOLD: float = 0.3  # Switch to survival below 30% HP.
_KNOCKBACK_RANGE: float = 6.0
_LEADERBOARD_INTERVAL: int = 30  # Ticks between leaderboard refreshes.


class HunterBot(ArenaBot):
    """Bounty hunter: find the #1 player and take them down."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="spear",
            stats={"hp": 4, "speed": 6, "attack": 6, "defense": 4},
            fallback="aggressive",
        )
        self._http_base: str = self._derive_http_url(server_url)
        self._target_id: str | None = None
        self._tick_count: int = 0

    @staticmethod
    def _derive_http_url(ws_url: str) -> str:
        """Convert wss://host/ws/bot -> https://host."""
        parsed = urlparse(ws_url)
        scheme = "https" if parsed.scheme == "wss" else "http"
        return f"{scheme}://{parsed.hostname}"

    async def _refresh_target(self) -> None:
        """Fetch leaderboard and set target to the #1 bot."""
        url = f"{self._http_base}/api/v1/leaderboard/?sort=elo&limit=1"
        try:
            async with aiohttp.ClientSession() as session:
                async with session.get(url, timeout=aiohttp.ClientTimeout(total=3)) as resp:
                    if resp.status == 200:
                        data: list[dict] = await resp.json()
                        if data:
                            self._target_id = data[0].get("bot_id")
        except Exception:
            pass  # Keep previous target on failure.

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]
        hp: int = state["hp"]
        max_hp: int = state["max_hp"]

        # Periodically refresh leaderboard target.
        self._tick_count += 1
        if self._tick_count % _LEADERBOARD_INTERVAL == 1:
            await self._refresh_target()

        # Survival mode: flee and grab health.
        if hp < max_hp * _SURVIVAL_THRESHOLD:
            health: list[dict] = [
                p for p in self.nearby_pickups(nearby)
                if p.get("pickup_type") == "health"
            ]
            if health:
                return self.move_toward(my_pos, health[0]["position"])
            enemy = self.closest_enemy(nearby)
            if enemy is not None:
                return self.move_away(my_pos, enemy["position"])
            return self.move_toward(my_pos, safe_zone["center"])

        # Try to find the #1 target among nearby bots.
        target: dict | None = None
        if self._target_id:
            for entity in nearby:
                if entity.get("type") == "bot" and entity.get("id") == self._target_id:
                    target = entity
                    break

        # Fallback: closest enemy.
        if target is None:
            target = self.closest_enemy(nearby)

        if target is None:
            return self.move_toward(my_pos, safe_zone["center"])

        dist: float = _dist(my_pos, target["position"])

        # Knockback: push enemies near zone edge outward.
        zone_dist: float = _dist(target["position"], safe_zone["center"])
        if dist < _KNOCKBACK_RANGE and zone_dist > safe_zone["radius"] * 0.7:
            return self.attack(target["id"])

        # Close in and attack.
        if dist < 4.0:
            return self.attack(target["id"])

        return self.move_toward(my_pos, target["position"])

    async def on_death(self, death_info: dict) -> None:
        pass

    async def on_respawn(self, respawn_info: dict) -> None:
        self._tick_count = 0  # Refresh leaderboard on next tick.

    async def on_round_end(self, round_info: dict) -> None:
        self._target_id = None


def _dist(a: list[float], b: list[float]) -> float:
    return ((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2) ** 0.5


if __name__ == "__main__":
    key: str = sys.argv[1]
    url: str = sys.argv[2] if len(sys.argv) > 2 else "wss://arena.angel-serv.com/ws/bot"
    bot = HunterBot(key, url)
    asyncio.run(bot.run())
