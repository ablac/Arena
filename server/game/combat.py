"""Combat processing for the game engine tick loop."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from server.config import settings
from server.game.obstacles import line_intersects_obstacle
from server.game.projectiles import create_projectile
from server.game.state import ActionType, StaffImpact
from server.game.weapons import (
    apply_cleave, apply_double_strike, apply_knockback,
    calculate_damage, get_weapon_config, is_in_range, is_ranged,
)

if TYPE_CHECKING:
    from server.game.state import BotState, Obstacle, Projectile

logger = logging.getLogger(__name__)


def process_combat(
    bots: dict[str, BotState],
    tick_rate: int,
    obstacles: list[Obstacle],
    projectiles: list[Projectile],
    staff_impacts: list[StaffImpact],
) -> list[dict]:
    """Process all attack actions simultaneously.

    Handles melee, ranged LOS, bow→projectile, staff→delayed area, stuns.
    """
    damage_queue: list[tuple[str, float, str, str]] = []
    events: list[dict] = []
    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.pending_action is None:
            continue
        if bot.stun_ticks > 0:
            continue
        if bot.pending_action.action_type != ActionType.ATTACK:
            continue

        target_id = bot.pending_action.target_id
        target_pos = bot.pending_action.target_position

        # Staff targets a position, not a bot
        if bot.weapon == "staff":
            if bot.cooldown_remaining > 0:
                continue
            pos = target_pos
            if pos is None and target_id and target_id in bots:
                pos = bots[target_id].position
            if pos is None or not isinstance(pos, tuple):
                continue
            _queue_staff_impact(bot, pos, staff_impacts, obstacles)
            cfg = get_weapon_config("staff")
            bot.cooldown_remaining = cfg["cooldown"]
            bot.round_shots_fired += 1
            continue

        if target_id is None or target_id not in bots:
            continue
        target = bots[target_id]
        if not target.is_alive:
            continue
        if bot.cooldown_remaining > 0:
            continue

        # Bow creates projectile instead of instant hit
        if bot.weapon == "bow":
            if not is_in_range(bot, target, "bow"):
                continue
            if line_intersects_obstacle(*bot.position, *target.position, obstacles):
                continue
            dmg = calculate_damage("bow", bot, target)
            proj = create_projectile(bot, target.position, dmg)
            projectiles.append(proj)
            cfg = get_weapon_config("bow")
            bot.cooldown_remaining = cfg["cooldown"]
            bot.round_shots_fired += 1
            continue

        # Melee weapons — no LOS check needed
        if not is_in_range(bot, target, bot.weapon):
            continue

        dmg = calculate_damage(bot.weapon, bot, target)
        damage_queue.append((target_id, dmg, bot_id, bot.weapon))
        bot.round_shots_fired += 1

        # Weapon specials
        if bot.weapon == "sword":
            for extra_id, extra_dmg in apply_cleave(bot, target, bots):
                damage_queue.append((extra_id, extra_dmg, bot_id, "sword"))
        elif bot.weapon == "daggers":
            for extra_id, extra_dmg in apply_double_strike(bot, target):
                damage_queue.append((extra_id, extra_dmg, bot_id, "daggers"))
        elif bot.weapon == "spear":
            bonus = apply_knockback(bot, target, obstacles)
            if bonus > 0:
                damage_queue.append((target_id, float(bonus), bot_id, "spear"))
        elif bot.weapon == "shield":
            target.stun_ticks = settings.combat.stun_duration_ticks

        cfg = get_weapon_config(bot.weapon)
        bot.cooldown_remaining = cfg["cooldown"]

    for target_id, dmg, attacker_id, weapon in damage_queue:
        _apply_damage(bots, target_id, dmg, attacker_id, weapon, events)
    _tick_timers(bots, tick_rate)
    return events


def process_staff_impacts(
    staff_impacts: list[StaffImpact], bots: dict[str, BotState]
) -> list[dict]:
    """Process delayed staff area attacks."""
    events: list[dict] = []
    to_remove: list[int] = []
    for i, impact in enumerate(staff_impacts):
        impact.ticks_remaining -= 1
        if impact.ticks_remaining > 0:
            continue
        to_remove.append(i)
        radius_sq = impact.radius ** 2
        for bid, bot in bots.items():
            if not bot.is_alive:
                continue
            dx = bot.position[0] - impact.position[0]
            dy = bot.position[1] - impact.position[1]
            if dx * dx + dy * dy <= radius_sq:
                _apply_damage(bots, bid, impact.damage, impact.owner_id, "staff", events)
                attacker = bots.get(impact.owner_id)
                if attacker:
                    attacker.round_shots_hit += 1

    for i in reversed(to_remove):
        staff_impacts.pop(i)
    return events


def _queue_staff_impact(
    attacker: BotState, pos: tuple[float, float],
    staff_impacts: list[StaffImpact], obstacles: list,
) -> None:
    """Queue a delayed staff area attack."""
    if line_intersects_obstacle(*attacker.position, *pos, obstacles):
        return
    cfg = get_weapon_config("staff")
    dmg = calculate_damage("staff", attacker, attacker)  # base damage calc
    staff_impacts.append(StaffImpact(
        owner_id=attacker.bot_id,
        position=pos,
        damage=dmg,
        radius=cfg["special_param"],
        ticks_remaining=settings.combat.staff_delay_ticks,
    ))


def _apply_damage(
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
    attacker = bots.get(attacker_id)
    if attacker:
        attacker.round_damage_dealt += int_dmg
        attacker.round_shots_hit += 1
    events.append({"type": "damage", "attacker": attacker_id,
                    "target": target_id, "weapon": weapon, "damage": int_dmg})


def _tick_timers(bots: dict[str, BotState], tick_rate: int) -> None:
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
