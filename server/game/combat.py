"""Combat processing for the game engine tick loop."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from server.config import settings
from server.game.damage import apply_damage, tick_timers
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


def _atk_result(bot, result, reason=None, **kw):
    r = {"action": "attack", "result": result, "weapon": bot.weapon, **kw}
    if reason:
        r["reason"] = reason
    bot.last_action_result = r


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
    view_radius_sq = settings.game.view_radius ** 2

    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.pending_action is None:
            continue
        if bot.stun_ticks > 0:
            if bot.pending_action.action_type == ActionType.ATTACK:
                _atk_result(bot, "miss", "stunned")
            continue
        if bot.pending_action.action_type != ActionType.ATTACK:
            continue

        target_id = bot.pending_action.target_id
        target_pos = bot.pending_action.target_position

        # Staff targets a position, not a bot
        if bot.weapon == "staff":
            if bot.cooldown_remaining > 0:
                _atk_result(bot, "miss", "on_cooldown", cooldown=round(bot.cooldown_remaining, 2))
                continue
            pos = target_pos
            if pos is None and target_id and target_id in bots:
                pos = bots[target_id].position
            if pos is None or not isinstance(pos, tuple):
                _atk_result(bot, "miss", "no_target")
                continue
            # Anti-cheat: staff target position must be within view radius
            dx = bot.position[0] - pos[0]
            dy = bot.position[1] - pos[1]
            if dx * dx + dy * dy > view_radius_sq:
                _atk_result(bot, "miss", "out_of_range")
                continue
            if not _queue_staff_impact(bot, pos, staff_impacts, obstacles):
                _atk_result(bot, "miss", "los_blocked")
                continue
            cfg = get_weapon_config("staff")
            bot.cooldown_remaining = cfg["cooldown"]
            bot.round_shots_fired += 1
            bot.last_action = "attack"
            bot.last_action_target = target_id
            _atk_result(bot, "fired")
            continue

        if target_id is None or target_id not in bots:
            _atk_result(bot, "miss", "invalid_target")
            continue
        target = bots[target_id]
        if not target.is_alive:
            _atk_result(bot, "miss", "target_dead")
            continue
        if bot.cooldown_remaining > 0:
            _atk_result(bot, "miss", "on_cooldown", cooldown=round(bot.cooldown_remaining, 2))
            continue

        # Anti-cheat: target must be within view radius
        dx = bot.position[0] - target.position[0]
        dy = bot.position[1] - target.position[1]
        if dx * dx + dy * dy > view_radius_sq:
            _atk_result(bot, "miss", "out_of_range")
            continue

        # Bow creates projectile instead of instant hit
        if bot.weapon == "bow":
            if not is_in_range(bot, target, "bow"):
                _atk_result(bot, "miss", "out_of_range")
                continue
            if line_intersects_obstacle(*bot.position, *target.position, obstacles):
                _atk_result(bot, "miss", "los_blocked")
                continue
            dmg = calculate_damage("bow", bot, target)
            proj = create_projectile(bot, target.position, dmg)
            projectiles.append(proj)
            cfg = get_weapon_config("bow")
            bot.cooldown_remaining = cfg["cooldown"]
            bot.round_shots_fired += 1
            bot.last_action = "attack"
            bot.last_action_target = target_id
            _atk_result(bot, "fired", target=target_id)
            continue

        # Melee weapons — no LOS check needed
        if not is_in_range(bot, target, bot.weapon):
            _atk_result(bot, "miss", "out_of_range")
            continue
        if target.invuln_ticks > 0:
            _atk_result(bot, "miss", "target_dodging")
            continue

        dmg = calculate_damage(bot.weapon, bot, target)
        damage_queue.append((target_id, dmg, bot_id, bot.weapon))
        bot.round_shots_fired += 1
        bot.last_action = "attack"
        bot.last_action_target = target_id
        _atk_result(bot, "hit", target=target_id, damage=round(dmg, 1))

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
        apply_damage(bots, target_id, dmg, attacker_id, weapon, events, obstacles)
    tick_timers(bots, tick_rate)
    return events


def process_shoves(
    bots: dict[str, BotState],
    obstacles: list[Obstacle],
) -> list[dict]:
    """Process all shove actions for the current tick.

    Shoves deal no damage but knock the target back significantly and apply a short stun.
    Uses its own cooldown separate from the weapon cooldown.
    """
    from server.game.obstacles import slide_along_obstacle

    events: list[dict] = []
    shove_range = settings.combat.shove_range
    shove_kb = settings.combat.shove_knockback
    shove_stun = settings.combat.shove_stun_ticks
    shove_cd = settings.combat.shove_cooldown

    for bot_id, bot in bots.items():
        if not bot.is_alive or bot.pending_action is None:
            continue
        if bot.stun_ticks > 0:
            continue
        if bot.pending_action.action_type != ActionType.SHOVE:
            continue

        target_id = bot.pending_action.target_id
        if target_id is None or target_id not in bots:
            bot.last_action_result = {"action": "shove", "result": "miss", "reason": "invalid_target"}
            continue
        target = bots[target_id]
        if not target.is_alive:
            bot.last_action_result = {"action": "shove", "result": "miss", "reason": "target_dead"}
            continue
        if bot.shove_cooldown > 0:
            bot.last_action_result = {"action": "shove", "result": "miss", "reason": "on_cooldown",
                                      "cooldown": round(bot.shove_cooldown, 2)}
            continue

        # Range check
        dx = bot.position[0] - target.position[0]
        dy = bot.position[1] - target.position[1]
        dist = (dx * dx + dy * dy) ** 0.5
        if dist > shove_range + settings.game.bot_radius * 2:
            bot.last_action_result = {"action": "shove", "result": "miss", "reason": "out_of_range"}
            continue

        # Invulnerable targets can't be shoved
        if target.invuln_ticks > 0:
            bot.last_action_result = {"action": "shove", "result": "miss", "reason": "target_dodging"}
            continue

        # Compute knockback direction (away from shover)
        if dist == 0:
            nx, ny = 1.0, 0.0
        else:
            nx = (target.position[0] - bot.position[0]) / dist
            ny = (target.position[1] - bot.position[1]) / dist

        new_x = target.position[0] + nx * shove_kb
        new_y = target.position[1] + ny * shove_kb

        # Slide along obstacles
        new_x, new_y = slide_along_obstacle(
            target.position[0], target.position[1], new_x, new_y,
            obstacles, radius=settings.game.bot_radius,
        )

        # Clamp to arena bounds
        w, h = float(settings.game.arena_width), float(settings.game.arena_height)
        new_x = max(0.0, min(w, new_x))
        new_y = max(0.0, min(h, new_y))

        target.position = (new_x, new_y)
        target.stun_ticks = max(target.stun_ticks, shove_stun)

        bot.shove_cooldown = shove_cd
        bot.last_action = "shove"
        bot.last_action_target = target_id
        bot.last_action_result = {"action": "shove", "result": "hit", "target": target_id}

        events.append({
            "type": "shove",
            "attacker": bot_id,
            "target": target_id,
            "knockback": shove_kb,
        })

    return events


def process_staff_impacts(
    staff_impacts: list[StaffImpact], bots: dict[str, BotState],
    obstacles: list[Obstacle] | None = None,
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
                dmg = impact.damage * (1.0 - bot.defense_reduction)
                if bot.weapon == "shield":
                    dmg *= 0.5
                apply_damage(bots, bid, dmg, impact.owner_id, "staff", events, obstacles or [])
                attacker = bots.get(impact.owner_id)
                if attacker:
                    attacker.round_shots_hit += 1

    for i in reversed(to_remove):
        staff_impacts.pop(i)
    return events


def _queue_staff_impact(
    attacker: BotState, pos: tuple[float, float],
    staff_impacts: list[StaffImpact], obstacles: list,
) -> bool:
    """Queue a delayed staff area attack. Returns True if queued, False if LOS blocked."""
    if line_intersects_obstacle(*attacker.position, *pos, obstacles):
        return False
    cfg = get_weapon_config("staff")
    from server.game.pickups import get_effective_damage_mult
    eff_mult = get_effective_damage_mult(attacker)
    base_dmg = cfg["damage"] * eff_mult
    staff_impacts.append(StaffImpact(
        owner_id=attacker.bot_id,
        position=pos,
        damage=base_dmg,
        radius=cfg["special_param"],
        ticks_remaining=settings.combat.staff_delay_ticks,
    ))
    return True
