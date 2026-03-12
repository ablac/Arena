"""Scavenger Bot - Pickup Collector.

A fast bot that prioritizes collecting pickups over fighting. With daggers
and max speed, it's hard to catch. Goes aggressive when a damage boost is active.

Usage: python scavenger.py <api_key> [server_url]
"""

import asyncio
import sys

from arena_sdk import ArenaBot

_ATTACK_RANGE: float = 4.0


class ScavengerBot(ArenaBot):
    """Loot goblin: collect everything, fight only when forced or buffed."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="daggers",
            stats={"hp": 3, "speed": 9, "attack": 5, "defense": 3},
            fallback="aggressive",
        )

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]
        effects: list[str] = state.get("active_effects", [])
        has_damage_boost: bool = "damage_boost" in effects

        enemy: dict | None = self.closest_enemy(nearby)
        pickups: list[dict] = self.nearby_pickups(nearby)

        # Damage boost active -- go aggressive while it lasts.
        if has_damage_boost and enemy is not None:
            dist: float = _dist(my_pos, enemy["position"])
            if dist < _ATTACK_RANGE:
                return self.attack(enemy["id"])
            return self.move_toward(my_pos, enemy["position"])

        # Priority 1: collect nearest pickup.
        if pickups:
            return self.move_toward(my_pos, pickups[0]["position"])

        # Priority 2: fight enemies only at point-blank range.
        if enemy is not None:
            dist = _dist(my_pos, enemy["position"])
            if dist < _ATTACK_RANGE:
                return self.attack(enemy["id"])

        # Nothing to do -- stay inside the safe zone.
        return self.move_toward(my_pos, safe_zone["center"])

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
    bot = ScavengerBot(key, url)
    asyncio.run(bot.run())
