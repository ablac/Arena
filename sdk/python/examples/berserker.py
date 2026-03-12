"""Berserker Bot - All Attack, No Brain.

A simple aggressive bot that charges the closest enemy and attacks relentlessly.
Good starter example for learning the ArenaBot SDK.

Usage: python berserker.py <api_key> [server_url]
"""

import asyncio
import sys

from arena_sdk import ArenaBot


class BerserkerBot(ArenaBot):
    """Pure aggression bot: move toward closest enemy, attack, never retreat."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="sword",
            stats={"hp": 3, "speed": 4, "attack": 10, "defense": 3},
            fallback="aggressive",
        )

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]

        enemy: dict | None = self.closest_enemy(nearby)
        if enemy is None:
            # No enemies visible — move toward safe zone center.
            return self.move_toward(my_pos, safe_zone["center"])

        distance: float = _dist(my_pos, enemy["position"])

        # Close enough to swing — attack.
        if distance < 3.0:
            return self.attack(enemy["id"])

        # Otherwise charge straight at them.
        return self.move_toward(my_pos, enemy["position"])

    async def on_death(self, death_info: dict) -> None:
        pass  # Berserkers don't mourn.

    async def on_respawn(self, respawn_info: dict) -> None:
        pass  # Back for more.

    async def on_round_end(self, round_info: dict) -> None:
        pass


def _dist(a: list[float], b: list[float]) -> float:
    return ((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2) ** 0.5


if __name__ == "__main__":
    key: str = sys.argv[1]
    url: str = sys.argv[2] if len(sys.argv) > 2 else "wss://angel-serv.com/ws/bot"
    bot = BerserkerBot(key, url)
    asyncio.run(bot.run())
