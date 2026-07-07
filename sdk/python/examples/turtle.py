"""Turtle Bot - The Immovable Object.

A territorial bot that picks a spot, defends it, and only moves to grab
health pickups when low on HP. High survivability, low kill count.

Usage: python turtle.py <api_key> [server_url]
"""

import asyncio
import sys

from arena_sdk import ArenaBot

_HP_THRESHOLD: float = 0.5  # Move for health when below 50% HP.
_ATTACK_RANGE: float = 5.0  # Engage enemies within this range.


class TurtleBot(ArenaBot):
    """Territorial tank: hold position, heal up, punish anyone who gets close."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="shield",
            stats={"hp": 10, "speed": 2, "attack": 3, "defense": 5},
            fallback="aggressive",
        )
        self.home: list[float] | None = None

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]
        hp: int = state["hp"]
        max_hp: int = state["max_hp"]

        # Establish home base on first tick.
        if self.home is None:
            self.home = list(my_pos)

        # Keep home inside the safe zone.
        if _dist(self.home, safe_zone["center"]) > safe_zone["radius"] * 0.6:
            self.home = list(safe_zone["center"])

        # Priority 1: seek health pickups when low.
        if hp < max_hp * _HP_THRESHOLD:
            health_pickups: list[dict] = [
                p for p in self.nearby_pickups(nearby)
                if p.get("pickup_type") == "health"
            ]
            if health_pickups:
                return self.move_toward(my_pos, health_pickups[0]["position"])

        # Priority 2: attack enemies that wander into our territory.
        enemy: dict | None = self.closest_enemy(nearby)
        if enemy is not None:
            dist: float = _dist(my_pos, enemy["position"])
            if dist < _ATTACK_RANGE:
                return self.attack(enemy["id"])

        # Priority 3: return home if we've drifted away.
        if _dist(my_pos, self.home) > 3.0:
            return self.move_toward(my_pos, self.home)

        return self.idle()

    async def on_death(self, death_info: dict) -> None:
        self.home = None  # Pick a new spot after respawn.

    async def on_respawn(self, respawn_info: dict) -> None:
        pass

    async def on_round_end(self, round_info: dict) -> None:
        self.home = None


def _dist(a: list[float], b: list[float]) -> float:
    return ((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2) ** 0.5


if __name__ == "__main__":
    key: str = sys.argv[1]
    url: str = sys.argv[2] if len(sys.argv) > 2 else "wss://arena.angel-serv.com/ws/bot"
    bot = TurtleBot(key, url)
    asyncio.run(bot.run())
