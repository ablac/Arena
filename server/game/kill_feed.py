"""Kill feed tracking — rolling log of recent kills."""

from __future__ import annotations

from collections import deque
from typing import Any

from server.config import settings
from server.game.state import KillFeedEntry


class KillFeed:
    """Maintains a rolling kill feed of the last N kills."""

    def __init__(self) -> None:
        self._entries: deque[KillFeedEntry] = deque(
            maxlen=settings.network.kill_feed_size
        )

    def add_kill(
        self, killer_name: str, victim_name: str, weapon: str, tick: int
    ) -> None:
        """Record a kill in the feed."""
        self._entries.append(KillFeedEntry(
            killer_name=killer_name,
            victim_name=victim_name,
            weapon=weapon,
            tick=tick,
        ))

    def get_recent(self, count: int = 5) -> list[dict[str, Any]]:
        """Get the last N kills as dicts for tick messages."""
        entries = list(self._entries)[-count:]
        return [
            {
                "killer": e.killer_name,
                "victim": e.victim_name,
                "weapon": e.weapon,
                "tick": e.tick,
            }
            for e in entries
        ]

    def get_all(self) -> list[dict[str, Any]]:
        """Get all kills in the feed for spectator broadcasts."""
        return [
            {
                "killer": e.killer_name,
                "victim": e.victim_name,
                "weapon": e.weapon,
                "tick": e.tick,
            }
            for e in self._entries
        ]

    def get_since(self, since_tick: int) -> list[dict[str, Any]]:
        """Get only kills that occurred after ``since_tick``."""
        return [
            {
                "killer": e.killer_name,
                "victim": e.victim_name,
                "weapon": e.weapon,
                "tick": e.tick,
            }
            for e in self._entries
            if e.tick > since_tick
        ]

    def clear(self) -> None:
        """Clear the kill feed."""
        self._entries.clear()
