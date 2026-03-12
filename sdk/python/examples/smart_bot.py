"""Smart Bot - The Thinker (Advanced).

A state-machine bot that dynamically switches between AGGRESSIVE, DEFENSIVE,
SCAVENGE, and ZONE_AWARE modes based on HP, nearby threats, pickups, and
safe-zone pressure. Evaluates threat levels and dodges incoming attacks.

Usage: python smart_bot.py <api_key> [server_url]
"""

import asyncio
import sys
from enum import Enum, auto

from arena_sdk import ArenaBot

_AGGRESSIVE_HP: float = 0.7
_DEFENSIVE_HP: float = 0.3
_SCAVENGE_HP: float = 0.5
_OUTNUMBER_THRESHOLD: int = 3
_OUTNUMBER_RANGE: float = 15.0
_ATTACK_RANGE: float = 5.0
_DODGE_RANGE: float = 7.0
_ZONE_SMALL: float = 30.0


class Mode(Enum):
    AGGRESSIVE = auto()
    DEFENSIVE = auto()
    SCAVENGE = auto()
    ZONE_AWARE = auto()


class SmartBot(ArenaBot):
    """Adaptive state-machine bot that picks the right strategy each tick."""

    def __init__(self, api_key: str, server_url: str) -> None:
        super().__init__(api_key, server_url)
        self.set_loadout(
            weapon="sword",
            stats={"hp": 5, "speed": 5, "attack": 5, "defense": 5},
            fallback="aggressive",
        )
        self.mode: Mode = Mode.AGGRESSIVE

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _dist(a: list[float], b: list[float]) -> float:
        return ((a[0] - b[0]) ** 2 + (a[1] - b[1]) ** 2) ** 0.5

    def _enemies_in_range(self, my_pos: list[float], nearby: list, radius: float) -> list[dict]:
        return [
            e for e in nearby
            if e.get("type") == "bot"
            and e.get("is_alive", True)
            and self._dist(my_pos, e["position"]) <= radius
        ]

    @staticmethod
    def _threat_level(enemy: dict) -> float:
        """Higher value = more dangerous."""
        hp_ratio: float = enemy.get("hp", 1) / max(enemy.get("max_hp", 1), 1)
        return (1.0 - hp_ratio) * 0.4 + 0.6  # Healthy enemies are still threats.

    def _incoming_projectile(self, my_pos: list[float], nearby: list) -> dict | None:
        """Return the closest projectile heading our way, if any."""
        projectiles: list[dict] = [
            e for e in nearby if e.get("type") == "projectile"
        ]
        if not projectiles:
            return None
        projectiles.sort(key=lambda p: self._dist(my_pos, p["position"]))
        closest = projectiles[0]
        if self._dist(my_pos, closest["position"]) < _DODGE_RANGE:
            return closest
        return None

    # ------------------------------------------------------------------
    # Mode selection
    # ------------------------------------------------------------------

    def _select_mode(
        self,
        hp_ratio: float,
        enemies_close: int,
        health_nearby: bool,
        zone_radius: float,
    ) -> Mode:
        # Zone pressure overrides everything when the circle is tiny.
        if zone_radius < _ZONE_SMALL:
            return Mode.ZONE_AWARE

        if hp_ratio < _DEFENSIVE_HP or enemies_close >= _OUTNUMBER_THRESHOLD:
            return Mode.DEFENSIVE

        if hp_ratio < _SCAVENGE_HP and health_nearby:
            return Mode.SCAVENGE

        if enemies_close > 0 and hp_ratio >= _AGGRESSIVE_HP:
            return Mode.AGGRESSIVE

        # Default: scavenge if pickups exist, otherwise zone-aware.
        return Mode.SCAVENGE if health_nearby else Mode.ZONE_AWARE

    # ------------------------------------------------------------------
    # Tick
    # ------------------------------------------------------------------

    async def on_tick(
        self, state: dict, nearby: list, safe_zone: dict
    ) -> dict:
        my_pos: list[float] = state["position"]
        hp: int = state["hp"]
        max_hp: int = state["max_hp"]
        hp_ratio: float = hp / max(max_hp, 1)

        enemies_close: list[dict] = self._enemies_in_range(my_pos, nearby, _OUTNUMBER_RANGE)
        health_pickups: list[dict] = [
            p for p in self.nearby_pickups(nearby)
            if p.get("pickup_type") == "health"
        ]

        self.mode = self._select_mode(
            hp_ratio, len(enemies_close), bool(health_pickups), safe_zone["radius"],
        )

        # Dodge incoming projectiles regardless of mode.
        projectile: dict | None = self._incoming_projectile(my_pos, nearby)
        if projectile is not None:
            return self.dodge(self.move_away(my_pos, projectile["position"])["direction"])

        # --- AGGRESSIVE ---
        if self.mode == Mode.AGGRESSIVE:
            target: dict | None = self.lowest_hp_enemy(nearby)
            if target is None:
                target = self.closest_enemy(nearby)
            if target is not None:
                if self._dist(my_pos, target["position"]) < _ATTACK_RANGE:
                    return self.attack(target["id"])
                return self.move_toward(my_pos, target["position"])

        # --- DEFENSIVE ---
        if self.mode == Mode.DEFENSIVE:
            if health_pickups:
                return self.move_toward(my_pos, health_pickups[0]["position"])
            enemy = self.closest_enemy(nearby)
            if enemy is not None:
                if self._dist(my_pos, enemy["position"]) < _ATTACK_RANGE:
                    return self.attack(enemy["id"])
                return self.move_away(my_pos, enemy["position"])
            return self.move_toward(my_pos, safe_zone["center"])

        # --- SCAVENGE ---
        if self.mode == Mode.SCAVENGE:
            pickups: list[dict] = self.nearby_pickups(nearby)
            if pickups:
                return self.move_toward(my_pos, pickups[0]["position"])
            enemy = self.closest_enemy(nearby)
            if enemy is not None and self._dist(my_pos, enemy["position"]) < _ATTACK_RANGE:
                return self.attack(enemy["id"])

        # --- ZONE_AWARE (default fallback) ---
        zone_dist: float = self._dist(my_pos, safe_zone["center"])
        if zone_dist > safe_zone["radius"] * 0.5:
            return self.move_toward(my_pos, safe_zone["center"])

        enemy = self.closest_enemy(nearby)
        if enemy is not None and self._dist(my_pos, enemy["position"]) < _ATTACK_RANGE:
            return self.attack(enemy["id"])

        return self.idle()

    async def on_death(self, death_info: dict) -> None:
        self.mode = Mode.AGGRESSIVE

    async def on_respawn(self, respawn_info: dict) -> None:
        self.mode = Mode.AGGRESSIVE

    async def on_round_end(self, round_info: dict) -> None:
        self.mode = Mode.AGGRESSIVE


if __name__ == "__main__":
    key: str = sys.argv[1]
    url: str = sys.argv[2] if len(sys.argv) > 2 else "wss://angel-serv.com/ws/bot"
    bot = SmartBot(key, url)
    asyncio.run(bot.run())
