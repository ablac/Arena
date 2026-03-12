"""Sniper Bot - Death from Afar.

Maintains distance from enemies, attacks the lowest-HP target in range,
and dodges when enemies close in. Prioritizes speed pickups to stay mobile.

Usage: python sniper.py <api_key> [server_url]
"""

import asyncio
import sys

from arena_sdk import ArenaBot

_IDEAL_MIN: float = 15.0
_IDEAL_MAX: float = 20.0
_DANGER_RANGE: float = 10.0


class SniperBot(ArenaBot):
    """Long-range kiter: stay far, shoot the weakest, grab speed boosts."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="bow",
            stats={"hp": 2, "speed": 8, "attack": 7, "defense": 3},
            fallback="aggressive",
        )

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]

        # Priority 1: grab speed pickups if nearby.
        speed_pickups: list[dict] = [
            p for p in self.nearby_pickups(nearby)
            if p.get("pickup_type") == "speed_boost"
        ]
        if speed_pickups:
            return self.move_toward(my_pos, speed_pickups[0]["position"])

        enemy: dict | None = self.closest_enemy(nearby)
        if enemy is None:
            return self.move_toward(my_pos, safe_zone["center"])

        dist: float = _dist(my_pos, enemy["position"])

        # Priority 2: dodge away if an enemy is dangerously close.
        if dist < _DANGER_RANGE:
            return self.move_away(my_pos, enemy["position"])

        # Priority 3: attack the lowest-HP enemy in range.
        weak: dict | None = self.lowest_hp_enemy(nearby)
        target: dict = weak if weak is not None else enemy

        if dist <= _IDEAL_MAX:
            return self.attack(target["id"])

        # Too far — close distance a little.
        return self.move_toward(my_pos, target["position"])

    async def on_death(self, death_info: dict) -> None:
        pass

    async def on_respawn(self, respawn_info: dict) -> None:
        pass

    async def on_round_end(self, round_info: dict) -> None:
        pass


def _dist(a: list[float], b: list[float]) -> float:
    return ((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2) ** 0.5


if __name__ == "__main__":
    key: str = sys.argv[1]
    url: str = sys.argv[2] if len(sys.argv) > 2 else "wss://angel-serv.com/ws/bot"
    bot = SniperBot(key, url)
    asyncio.run(bot.run())
