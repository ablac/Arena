"""Damage application, hit knockback, and timer tick helpers.

Extracted from combat.py to keep files under 200 lines.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from server.game.state import BotState

_HIT_KNOCKBACK = 2.5


def apply_damage(
    bots: dict[str, BotState], target_id: str, dmg: float,
    attacker_id: str, weapon: str, events: list[dict],
) -> None:
    """Apply damage to a target, respecting invulnerability and shield absorb."""
    target = bots.get(target_id)
    if target is None or not target.is_alive or target.invuln_ticks > 0:
        return
    int_dmg = int(round(dmg))
    if target.shield_absorb > 0:
        absorbed = min(target.shield_absorb, int_dmg)
        target.shield_absorb -= absorbed
        int_dmg -= absorbed
    target.hp -= int_dmg
    target.round_damage_taken += int_dmg
    target.last_damaged_by = attacker_id
    target.hits_received.append({"from": attacker_id, "damage": int_dmg, "weapon": weapon})
    attacker = bots.get(attacker_id)
    if attacker:
        attacker.round_damage_dealt += int_dmg
        attacker.round_shots_hit += 1
        _apply_hit_knockback(attacker, target)
    events.append({"type": "damage", "attacker": attacker_id,
                    "target": target_id, "weapon": weapon, "damage": int_dmg})


def _apply_hit_knockback(attacker: BotState, target: BotState) -> None:
    """Push target away from attacker on hit, respecting obstacles and arena bounds."""
    from server.config import settings
    from server.game.obstacles import slide_along_obstacle

    dx = target.position[0] - attacker.position[0]
    dy = target.position[1] - attacker.position[1]
    dist = (dx * dx + dy * dy) ** 0.5
    if dist == 0:
        return
    nx, ny = dx / dist, dy / dist
    new_x = target.position[0] + nx * _HIT_KNOCKBACK
    new_y = target.position[1] + ny * _HIT_KNOCKBACK
    # Clamp to arena bounds
    w, h = float(settings.game.arena_width), float(settings.game.arena_height)
    new_x = max(0.0, min(w, new_x))
    new_y = max(0.0, min(h, new_y))
    target.position = (new_x, new_y)


def tick_timers(bots: dict[str, BotState], tick_rate: int) -> None:
    """Reduce cooldowns, stun, invulnerability, and dodge cooldown timers."""
    dt = 1.0 / tick_rate
    for bot in bots.values():
        if bot.cooldown_remaining > 0:
            bot.cooldown_remaining = max(0.0, bot.cooldown_remaining - dt)
        if bot.stun_ticks > 0:
            bot.stun_ticks -= 1
        if bot.invuln_ticks > 0:
            bot.invuln_ticks -= 1
        if bot.dodge_cooldown > 0:
            bot.dodge_cooldown -= 1
