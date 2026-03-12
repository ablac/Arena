"""Pydantic v2 models for all WebSocket protocol messages.

Defines the message schemas exchanged between the server and bot clients
over WebSocket connections during an AI Battle Arena session.
"""

from typing import Literal

from pydantic import BaseModel, ValidationError


# ---------------------------------------------------------------------------
# Server -> Bot messages
# ---------------------------------------------------------------------------


class ConnectedMessage(BaseModel):
    """Sent to a bot immediately after it connects and authenticates."""

    type: Literal["connected"] = "connected"
    bot_id: str
    arena_size: tuple[int, int]
    available_weapons: list[str]
    stat_budget: int
    stat_min: int
    stat_max: int
    timeout_seconds: int
    last_loadout: dict | None = None  # weapon + stats from bot config


class LoadoutConfirmedMessage(BaseModel):
    """Confirms the bot's chosen loadout and assigns a spawn position."""

    type: Literal["loadout_confirmed"] = "loadout_confirmed"
    weapon: str
    stats: dict[str, int]
    computed: dict[str, float]  # max_hp, move_speed, attack_mult, defense_red
    position: tuple[float, float]


class TickMessage(BaseModel):
    """Per-tick game-state snapshot delivered to each bot."""

    type: Literal["tick"] = "tick"
    tick_number: int
    your_state: dict
    nearby_entities: list[dict]
    safe_zone: dict
    view_radius: int


class DeathMessage(BaseModel):
    """Notifies a bot that it has been killed."""

    type: Literal["death"] = "death"
    killed_by: str
    weapon_used: str
    damage: float
    your_kills_this_life: int
    respawn_in_seconds: int


class RespawnMessage(BaseModel):
    """Notifies a bot that it has respawned."""

    type: Literal["respawn"] = "respawn"
    position: tuple[float, float]
    hp: int


class RoundEndMessage(BaseModel):
    """Sent at the end of each round with summary statistics."""

    type: Literal["round_end"] = "round_end"
    round_number: int
    your_stats: dict
    round_winner: str | None
    next_round_in: int


class ErrorMessage(BaseModel):
    """Generic error sent to the bot."""

    type: Literal["error"] = "error"
    message: str


class KickMessage(BaseModel):
    """Sent just before forcibly disconnecting a bot."""

    type: Literal["kick"] = "kick"
    reason: str


# ---------------------------------------------------------------------------
# Bot -> Server messages
# ---------------------------------------------------------------------------


class LoadoutSelectMessage(BaseModel):
    """Bot's chosen weapon, stat allocation, and fallback AI behaviour."""

    type: Literal["select_loadout"] = "select_loadout"
    weapon: str
    stats: dict[str, int]
    fallback_behavior: str = "aggressive"


class ActionMessage(BaseModel):
    """Per-tick action submitted by a bot.

    Supported actions: move, attack, dodge, use_item, idle.
    """

    type: Literal["action"] = "action"
    tick: int
    action: str  # move, attack, dodge, use_item, idle
    target: str | None = None
    direction: tuple[float, float] | None = None
    item_id: str | None = None


# ---------------------------------------------------------------------------
# Union types for convenience
# ---------------------------------------------------------------------------

ServerMessage = (
    ConnectedMessage
    | LoadoutConfirmedMessage
    | TickMessage
    | DeathMessage
    | RespawnMessage
    | RoundEndMessage
    | ErrorMessage
    | KickMessage
)

BotMessage = LoadoutSelectMessage | ActionMessage


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def parse_bot_message(data: dict) -> LoadoutSelectMessage | ActionMessage | None:
    """Parse incoming bot message into the appropriate model.

    Returns ``None`` if the message type is unrecognised or validation fails.
    """
    msg_type = data.get("type")
    try:
        if msg_type == "select_loadout":
            return LoadoutSelectMessage.model_validate(data)
        if msg_type == "action":
            return ActionMessage.model_validate(data)
    except ValidationError:
        return None
    return None
