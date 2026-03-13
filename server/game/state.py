"""In-memory game state data structures."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum

from fastapi import WebSocket


class ActionType(str, Enum):
    """Valid bot action types."""

    MOVE = "move"
    MOVE_TO = "move_to"
    ATTACK = "attack"
    DODGE = "dodge"
    USE_ITEM = "use_item"
    IDLE = "idle"


class FallbackBehavior(str, Enum):
    """Fallback AI behavior modes."""

    AGGRESSIVE = "aggressive"
    DEFENSIVE = "defensive"
    OPPORTUNISTIC = "opportunistic"
    TERRITORIAL = "territorial"
    HUNTER = "hunter"


@dataclass
class Action:
    """A bot's action for the current tick."""

    action_type: ActionType
    target_id: str | None = None
    direction: tuple[float, float] | None = None
    item_id: str | None = None
    target_position: tuple[float, float] | None = None


@dataclass
class Effect:
    """An active status effect on a bot."""

    name: str
    remaining_ticks: int
    value: float = 0.0


@dataclass
class BotState:
    """Live state of a connected bot in the arena."""

    bot_id: str
    api_key_id: str
    name: str
    position: tuple[float, float] = (0.0, 0.0)
    hp: int = 100
    max_hp: int = 100
    speed: float = 3.0
    attack_multiplier: float = 1.0
    defense_reduction: float = 0.0
    weapon: str = "sword"
    cooldown_remaining: float = 0.0
    is_alive: bool = True
    kill_streak: int = 0
    active_effects: list[Effect] = field(default_factory=list)
    fallback_behavior: str = "aggressive"
    last_action_tick: int = 0
    pending_action: Action | None = None
    websocket: WebSocket | None = None
    respawn_timer: float = 0.0
    spawn_position: tuple[float, float] = (0.0, 0.0)
    avatar_color: str = "#FFFFFF"
    stats: dict[str, int] = field(
        default_factory=lambda: {"hp": 5, "speed": 5, "attack": 5, "defense": 5}
    )
    # Dodge
    dodge_cooldown: int = 0
    invuln_ticks: int = 0
    # Stun
    stun_ticks: int = 0
    # Shield bubble absorb
    shield_absorb: int = 0
    # ELO rating (in-memory, synced to DB)
    elo: int = 1000
    # Last bot that dealt damage to us (for kill attribution)
    last_damaged_by: str | None = None
    # Persistent action fields — survive until consumed by spectator broadcast
    last_action: str | None = None
    last_action_target: str | None = None
    # Pathfinding (move_to)
    current_path: list[tuple[float, float]] = field(default_factory=list)
    path_target: tuple[float, float] | None = None
    # Round stats (reset each round)
    round_kills: int = 0
    round_deaths: int = 0
    round_damage_dealt: float = 0.0
    round_damage_taken: float = 0.0
    round_distance: float = 0.0
    round_shots_fired: int = 0
    round_shots_hit: int = 0
    round_longest_life: int = 0
    round_life_start_tick: int = 0
    round_pickups: int = 0


@dataclass
class Pickup:
    """An item pickup on the arena floor."""

    pickup_id: str
    pickup_type: str
    position: tuple[float, float] = (0.0, 0.0)
    value: float = 0.0


@dataclass
class Projectile:
    """An in-flight projectile (bow arrow)."""

    projectile_id: str
    owner_id: str
    position: tuple[float, float]
    direction: tuple[float, float]
    speed: float
    damage: float
    weapon: str
    age_ticks: int = 0
    max_age_ticks: int = 10


@dataclass
class Obstacle:
    """A static rectangular obstacle in the arena."""

    x: float
    y: float
    width: float
    height: float


@dataclass
class StaffImpact:
    """A pending staff area attack (delayed by 2 ticks)."""

    owner_id: str
    position: tuple[float, float]
    damage: float
    radius: float
    ticks_remaining: int


@dataclass
class KillFeedEntry:
    """A single entry in the kill feed."""

    killer_name: str
    victim_name: str
    weapon: str
    tick: int


@dataclass
class RoundState:
    """State of the current game round."""

    round_number: int = 0
    start_tick: int = 0
    is_active: bool = False
    time_remaining: float = 0.0
    in_intermission: bool = False
    intermission_ticks: int = 0
    in_lobby: bool = True
    lobby_countdown_ticks: int = 0
