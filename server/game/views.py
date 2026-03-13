"""Read-only view helpers for the game engine state."""

from __future__ import annotations

from typing import Any, TYPE_CHECKING

if TYPE_CHECKING:
    from server.game.state import BotState, Pickup


def bot_to_nearby_dict(bot: BotState) -> dict[str, Any]:
    """Serialize a bot for the nearby_entities list in tick messages."""
    return {
        "bot_id": bot.bot_id,
        "name": bot.name,
        "position": bot.position,
        "hp": bot.hp,
        "max_hp": bot.max_hp,
        "weapon": bot.weapon,
        "is_alive": bot.is_alive,
        "avatar_color": bot.avatar_color,
    }


def build_spectator_state(
    tick: int,
    bots: dict[str, BotState],
    pickups: list[Pickup],
    kill_feed: list[dict] | None = None,
    obstacles: list[dict] | None = None,
) -> dict[str, Any]:
    """Build the full arena state dict for spectator broadcasts."""
    return {
        "type": "arena_state",
        "tick": tick,
        "bots": [
            {
                "bot_id": b.bot_id, "name": b.name, "position": b.position,
                "hp": b.hp, "max_hp": b.max_hp, "weapon": b.weapon,
                "is_alive": b.is_alive, "kill_streak": b.kill_streak,
                "avatar_color": b.avatar_color,
                "action": b.last_action,
                "target_id": b.last_action_target,
            }
            for b in bots.values()
        ],
        "safe_zone": None,
        "pickups": [
            {"pickup_id": p.pickup_id, "type": p.pickup_type, "position": p.position}
            for p in pickups
        ],
        "kill_feed": kill_feed or [],
        "obstacles": obstacles or [],
    }


def build_arena_status(
    running: bool, bots: dict[str, BotState], round_number: int,
    tick: int,
) -> dict[str, Any]:
    """Build arena status for the REST endpoint."""
    alive = sum(1 for b in bots.values() if b.is_alive)
    return {
        "status": "active" if running else "stopped",
        "bots_connected": len(bots),
        "bots_alive": alive,
        "round_number": round_number,
        "tick": tick,
    }
