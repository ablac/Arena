'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
 * Detects attacks from spectator state and triggers weapon swing + hit effects.
 * @module renderer/bots
 */

import { createBotEntry, disposeBotEntry } from './bot-body.js?v=20260718o';
import {
  forgeContactDelay,
  updateForgeCharacter,
  triggerForgeAttack,
  triggerForgeDodge,
  triggerForgeHit,
  triggerForgeShove,
} from './character-anims.js?v=20260714e';
import {setForgeChassisLighting, updateForgeCharacterLOD} from './character-rig.js?v=20260718o';
import { applyBotCosmetics, disposeBotCosmetics } from './cosmetics.js?v=20260714e';
import {bodyFormKeyForBot} from './body-form-roster.js?v=20260714e';
import {
  hideWorldTaunt,
  hideWorldSelection,
  scaleWorldBotHud,
  setWorldBotHudVisible,
  showWorldTaunt,
  showWorldSelection,
  updateWorldBotHudHealth,
  worldHudScaleForRadius,
} from './world-hud.js?v=20260718o';
import { isEnabled } from '../settings.js';

export const BODY_FORM_NEAR_CHARACTER_LIMIT = 64;
const BODY_FORM_LOD_SELECTION_INTERVAL_MS = 250;

export function cooldownActionStarted(action, previousAction, cooldown, previousCooldown, expectedAction) {
  const current = Number.isFinite(cooldown) ? cooldown : 0;
  const previous = Number.isFinite(previousCooldown) ? previousCooldown : 0;
  return action === expectedAction && (
    current > previous + 0.35 ||
    (previousAction !== expectedAction && current > 0.05)
  );
}

export function actionTickStarted(action, expectedAction, actionTick, previousActionTick, fallbackStarted) {
  if (action !== expectedAction) return false;
  const currentKnown = Number.isFinite(actionTick) && actionTick > 0;
  if (currentKnown && Number.isFinite(previousActionTick)) {
    return actionTick !== previousActionTick;
  }
  return fallbackStarted === true;
}

/** Resolve construction-time body geometry without letting a disabled skin leak through. */
export function bodyFormRenderState(bot, skinsEnabled = true) {
  const configuredKey = bodyFormKeyForBot(bot);
  if (skinsEnabled || configuredKey === 'standard') {
    return {bodyFormKey: configuredKey, renderBot: bot};
  }
  const cosmetics = bot?.cosmetics && typeof bot.cosmetics === 'object' ? bot.cosmetics : {};
  return {
    bodyFormKey: 'standard',
    renderBot: {...bot, cosmetics: {...cosmetics, bot_skin: 'standard'}},
  };
}

/** Keep only the closest bounded set of live full-body rigs at near detail. */
export function selectNearBodyFormIDs(entries, camera, limit = BODY_FORM_NEAR_CHARACTER_LIMIT, selectedBotId = '') {
  const cap = Math.max(0, Math.floor(Number(limit) || 0));
  if (!entries || cap === 0) return new Set();
  const focus = camera?.target || camera?.globalPosition || camera?.position || {x: 0, z: 0};
  const focusX = Number(focus.x) || 0;
  const focusZ = Number(focus.z) || 0;
  const candidates = [];
  for (const [id, entry] of entries) {
    if (!entry || entry.presentationOnly || !entry.bodyFormKey || entry.bodyFormKey === 'standard') continue;
    const rootEnabled = typeof entry.root?.isEnabled === 'function' ? entry.root.isEnabled() : true;
    // Completed corpse animations remain in the renderer map until the next
    // snapshot removes them. Once their root is hidden they must not consume a
    // scarce near-detail slot; still-visible death animations remain eligible.
    if (entry.isAlive === false && rootEnabled === false) continue;
    const position = entry.root?.position;
    const x = Number(position?.x ?? entry.currPos?.[0]) || 0;
    const z = Number(position?.z ?? entry.currPos?.[1]) || 0;
    const dx = x - focusX;
    const dz = z - focusZ;
    candidates.push({
      id,
      selected: id === selectedBotId,
      alive: entry.isAlive !== false,
      distanceSquared: dx * dx + dz * dz,
    });
  }
  candidates.sort((left, right) =>
    Number(right.selected) - Number(left.selected) ||
    Number(right.alive) - Number(left.alive) ||
    left.distanceSquared - right.distanceSquared ||
    String(left.id).localeCompare(String(right.id))
  );
  return new Set(candidates.slice(0, cap).map(candidate => candidate.id));
}

function forEachForgeStatusMaterial(entry, visit) {
  const materials = entry?._forgeStatusMaterials;
  if (Array.isArray(materials) && materials.length) {
    for (let index = 0; index < materials.length; index += 1) {
      if (materials[index]) visit(materials[index], index);
    }
    return;
  }
  if (entry?.bodyMat) visit(entry.bodyMat, 0);
  if (entry?.headMat && entry.headMat !== entry.bodyMat) visit(entry.headMat, 1);
}

export class BotRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.entries = new Map();
    this._lastFrame = performance.now();
    this._motionQuery = typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null;
    /** @type {Function|null} callback(attackerX, attackerZ, targetX, targetZ, color, weapon) */
    this.onAttack = null;
    /** @type {Function|null} callback(x, z, color) */
    this.onDodge = null;
    /** @type {Function|null} callback(attackerX, attackerZ, targetX, targetZ, color) */
    this.onShove = null;
    /** @type {Function|null} callback(attackerX, attackerZ, targetX, targetZ) */
    this.onGrapple = null;
    this.onSelectionChange = null;
    this.selectedBotId = null;
    this._bodyFormNearIDs = new Set();
    this._bodyFormLODRefreshAt = 0;
    this._bodyFormLODSelectedBotId = '';
  }

  _disposeTaunt(entry) {
    if (!entry?.worldHud) return;
    hideWorldTaunt(entry.worldHud, true);
  }

  update(bots) {
    const seen = new Set();
    const now = performance.now();
    const skinsEnabled = isEnabled('botCosmetics', 'skins');

    // Build position lookup lazily — only when an attack needs it
    let posMap = null;
    const getPosMap = () => {
      if (!posMap) {
        posMap = new Map();
        for (const b of bots) posMap.set(b.bot_id, b.position);
      }
      return posMap;
    };

    for (const bot of bots) {
      seen.add(bot.bot_id);
      let entry = this.entries.get(bot.bot_id);
      const weaponType = bot.weapon || 'sword';
      const bodyFormState = bodyFormRenderState(bot, skinsEnabled);
      const bodyFormKey = bodyFormState.bodyFormKey;

      // A bot may change its configured loadout between rounds. The old
      // renderer kept the first mesh forever, so the server could report a
      // bow while spectators still saw a sword. Rebuild only at that explicit
      // chassis boundary; normal snapshots continue reusing the entry.
      if (entry?.profile?.weapon !== weaponType || entry?.bodyFormKey !== bodyFormKey) {
        if (entry) {
          this._disposeTaunt(entry);
          disposeBotCosmetics(entry);
          disposeBotEntry(entry);
        }
        entry = createBotEntry(bodyFormState.renderBot, this.scene);
        this.entries.set(bot.bot_id, entry);
      }
      entry.botData = bot;
      applyBotCosmetics(entry, bot, this.scene);

      // Entity interpolation: store last two server positions + timing.
      if (!entry._interpReady) {
        // First appearance — snap immediately.
        entry.root.position.x = bot.position[0];
        entry.root.position.z = bot.position[1];
        entry.prevPos = [bot.position[0], bot.position[1]];
        entry.currPos = [bot.position[0], bot.position[1]];
        entry._interpStart = now;
        entry._interpDur = 100;
        entry._interpReady = true;
      } else {
        // A respawn keeps the entry (it was only disabled while dead), so a
        // teleport to the spawn point would otherwise smooth across the arena
        // through walls for ~0.5s with the respawn glow on. Re-snap on any
        // jump far larger than a server-tick of movement.
        const jx = bot.position[0] - entry.currPos[0];
        const jz = bot.position[1] - entry.currPos[1];
        if (jx * jx + jz * jz > 150 * 150) {
          entry._interpReady = false;
        } else {
          entry.prevPos = [entry.currPos[0], entry.currPos[1]];
          entry.currPos = [bot.position[0], bot.position[1]];
          // Measure actual interval between server updates for accurate lerp.
          const elapsed = now - entry._interpStart;
          if (elapsed > 30) entry._interpDur = elapsed;
          entry._interpStart = now;
        }
      }

      // Death-transition bookkeeping must precede the visibility decision so
      // the corpse window opens on the SAME tick the bot dies (stamping it
      // later in the pass would blink the corpse off for one tick first).
      // The killer scan runs here because the contact-synced hit stamp
      // arrives ~0.25s AFTER death detection for melee; bookkeeping stays
      // live regardless of the toggle so mid-round enabling needs no warmup.
      if (!bot.is_alive && entry._wasAlive) {
        for (const other of bots) {
          if (other.is_alive && other.target_id === bot.bot_id) {
            entry._hitFromX = other.position[0];
            entry._hitFromZ = other.position[1];
            entry._hitFromT = now;
            break;
          }
        }
        // Wall-clock HARD ceiling only; the primary hide trigger is the
        // death anim completing (anim clock), so a throttled tab cannot
        // hide the corpse mid-fall nor strand it forever.
        entry._corpseUntil = isEnabled('deathEffects', 'directionalDeath')
          ? now + 3000 : 0;
      }
      // Visibility. The corpse window keeps a freshly-dead body visible so
      // the death choreography can actually be seen (it used to run on a
      // hidden node); wall-clock so a throttled tab still hides on time.
      const fallDone = entry.anim && entry.anim.deathTimer >= 0.88;
      entry.root.setEnabled(bot.is_alive ||
        (!fallDone && (entry._corpseUntil || 0) > now));
      setWorldBotHudVisible(entry.worldHud, bot.is_alive);

      // HP bar — only update when HP changes
      if (bot.hp !== entry._lastHp) {
        // Damage flinch: when HP actually dropped on a live bot, arm a brief
        // recoil punch. _lastHp is null on first appearance so a spawn never
        // flinches; _wasAlive still holds last frame's state here.
        if (entry._lastHp != null && bot.hp < entry._lastHp && bot.is_alive && entry._wasAlive) {
          const dmg = entry._lastHp - bot.hp;
          entry._flinch = Math.min(1, 0.35 + dmg / 30); // intensity 0.35..1 by hit size
          entry._flinchT = 0.18;                         // seconds, decays in interpolate()
          // Directional reaction: face the recoil AWAY from the attacker when
          // the hit source is fresh (set by the engine's contact-synced effect
          // path or event handlers); otherwise assume a frontal hit.
          const fresh = entry._hitFromT && (now - entry._hitFromT) < 400;
          entry._hitYaw = fresh
            ? Math.atan2(entry._hitFromX - bot.position[0], entry._hitFromZ - bot.position[1])
            : entry.root.rotation.y;
          triggerForgeHit(entry.anim, entry._flinch);
        }
        entry._lastHp = bot.hp;
        const hpRatio = bot.hp / bot.max_hp;
        updateWorldBotHudHealth(entry.worldHud, hpRatio);
        // Wound level drives the Forge idle slump: 0 healthy,
        // 1 wounded (<35%), 2 critical (<15%). Recomputed on every HP change,
        // so a heal or respawn (HP back up) clears it automatically.
        if (entry.anim) {
          entry.anim.woundLevel = hpRatio < 0.15 ? 2 : (hpRatio < 0.35 ? 1 : 0);
        }
      }

      // Status effect visuals (dodge transparency, stun tint)
      this._updateStatusEffects(entry, bot, now);

      // Attack detection precedes animation so the new pose starts this frame.
      const botAction = bot.action || bot.last_action; // server sends last_action
      const liveActionTick = Number(bot.last_action_tick);
      const prevActionTick = entry._lastActionTick;
      const liveCooldown = Number(bot.cooldown_remaining || 0);
      const prevCooldown = Number(entry._lastCooldown ?? 0);
      const attackJustStarted = bot.is_alive && entry._wasAlive && actionTickStarted(
        botAction, 'attack', liveActionTick, prevActionTick,
        cooldownActionStarted(botAction, entry._lastAction, liveCooldown, prevCooldown, 'attack'),
      );

      if (attackJustStarted) {
        triggerForgeAttack(entry.anim, weaponType, liveCooldown);

        // Face toward target (smoothed via anim.targetRotY)
        const explicitTargetPos = Array.isArray(bot.target_position) ? bot.target_position : null;
        const targetPos = explicitTargetPos || (bot.target_id ? getPosMap().get(bot.target_id) : null);
        if (targetPos) {
          const adx = targetPos[0] - bot.position[0];
          const adz = targetPos[1] - bot.position[1];
          if (adx !== 0 || adz !== 0) {
            entry.anim.targetRotY = Math.atan2(adx, adz);
          }
          // Notify effects system with weapon type. contactDelay lets the
          // engine land impact effects and the victim reaction at the moment
          // the swing visually connects instead of at swing start.
          if (this.onAttack) {
            const contactDelay = forgeContactDelay(weaponType, liveCooldown);
            this.onAttack(bot.position[0], bot.position[1],
                          targetPos[0], targetPos[1], bot.avatar_color, weaponType, {
                            targetId: bot.target_id || null,
                            targetPosition: explicitTargetPos,
                            contactDelay,
                          });
          }
        }
      }

      // Shove detection
      const shoveJustStarted = bot.is_alive && entry._wasAlive && actionTickStarted(
        botAction, 'shove', liveActionTick, prevActionTick,
        botAction === 'shove' && entry._lastAction !== 'shove',
      );
      if (shoveJustStarted) {
        triggerForgeShove(entry.anim);

        const targetPos = bot.target_id ? getPosMap().get(bot.target_id) : null;
        if (targetPos) {
          const adx = targetPos[0] - bot.position[0];
          const adz = targetPos[1] - bot.position[1];
          if (adx !== 0 || adz !== 0) {
            entry.anim.targetRotY = Math.atan2(adx, adz);
          }
          if (this.onShove) {
            this.onShove(bot.position[0], bot.position[1],
                         targetPos[0], targetPos[1], bot.avatar_color);
          }
        }
      }

      // Grapple detection. LastActionResult remains "grapple" across many
      // snapshots, so the cooldown edge prevents replaying the pose/effect.
      const liveGrappleCooldown = Number(bot.grapple_cooldown || 0);
      const prevGrappleCooldown = Number(entry._lastGrappleCooldown ?? 0);
      const grappleJustStarted = bot.is_alive && entry._wasAlive && actionTickStarted(
        botAction, 'grapple', liveActionTick, prevActionTick,
        cooldownActionStarted(
          botAction, entry._lastAction,
          liveGrappleCooldown, prevGrappleCooldown, 'grapple',
        ),
      );
      if (grappleJustStarted) {
        const explicitTargetPos = Array.isArray(bot.target_position) ? bot.target_position : null;
        const targetPos = explicitTargetPos || (bot.target_id ? getPosMap().get(bot.target_id) : null);
        if (targetPos) {
          const adx = targetPos[0] - bot.position[0];
          const adz = targetPos[1] - bot.position[1];
          if (adx !== 0 || adz !== 0) entry.anim.targetRotY = Math.atan2(adx, adz);
          triggerForgeAttack(entry.anim, 'grapple', 0.52, true);
          if (this.onGrapple) {
            this.onGrapple(bot.position[0], bot.position[1], targetPos[0], targetPos[1]);
          }
        }
      }

      // Dodge detection
      const dodgeJustStarted = bot.is_alive && entry._wasAlive && actionTickStarted(
        botAction, 'dodge', liveActionTick, prevActionTick,
        botAction === 'dodge' && bot.is_dodging && !entry._lastDodging,
      );
      if (dodgeJustStarted) {
        triggerForgeDodge(entry.anim, entry.anim.moveAngle);
        if (this.onDodge) {
          this.onDodge(bot.position[0], bot.position[1], bot.avatar_color);
        }
      }

      // Death flash
      if (!bot.is_alive && entry._wasAlive) {
        if (isEnabled('deathEffects', 'deathFlash')) this._deathFlash(entry);
        // Release the flinch channel so a respawn starts at rest scale,
        // and zero the hit-reaction rotations so a respawn stands straight.
        entry._flinchT = 0;
        entry.root.scaling.setAll(1);
      }
      entry._wasAlive = bot.is_alive;
      entry.isAlive = bot.is_alive;
      entry._lastCooldown = liveCooldown;
      entry._lastGrappleCooldown = liveGrappleCooldown;
      entry._lastActionTick = Number.isFinite(liveActionTick) ? liveActionTick : null;
      entry._lastAction = botAction;
      entry._lastDodging = !!bot.is_dodging;
    }

    for (const [id, entry] of this.entries) {
      if (!seen.has(id)) {
        if (this.selectedBotId === id) this.clearSelection();
        this._disposeTaunt(entry);
        disposeBotCosmetics(entry);
        disposeBotEntry(entry);
        this.entries.delete(id);
      }
    }

    this._refreshSelectionPanel();
  }

  /**
   * Remove one bot entry immediately (intermission despawn, issue #189).
   * Uses the exact disposal path the snapshot sweep uses for departed bots,
   * so the next update() rebuilds the entry from scratch — no lifted root,
   * hidden label, or stale interpolation state can leak into the next round.
   */
  despawnEntry(botId) {
    const entry = this.entries.get(botId);
    if (!entry) return;
    if (this.selectedBotId === botId) this.clearSelection();
    this._disposeTaunt(entry);
    disposeBotCosmetics(entry);
    disposeBotEntry(entry);
    this.entries.delete(botId);
  }

  /** Snap to the newest authoritative positions after frame suspension. */
  resume() {
    const now = performance.now();
    this._lastFrame = now;
    for (const [, entry] of this.entries) {
      if (!entry._interpReady || !Array.isArray(entry.currPos)) continue;
      const x = entry.currPos[0];
      const z = entry.currPos[1];
      entry.root.position.x = x;
      entry.root.position.z = z;
      entry.prevPos = [x, z];
      entry._interpStart = now;
      entry._poseX = x;
      entry._poseZ = z;
    }
  }

  /**
   * Called every render frame — linearly interpolates bot positions between
   * the last two server snapshots at constant speed, then ticks animations.
   */
  interpolate() {
    const now = performance.now();
    const dt = Math.min((now - this._lastFrame) / 1000, 0.1);
    this._lastFrame = now;

    // Live rendering.characterLighting flip (issue #181): the shared chassis
    // and weapon materials carry both emissive variants, so flipping is a
    // handful of material writes that only run on an actual toggle edge —
    // the steady-state per-frame cost is this one isEnabled() comparison.
    const chassisLit = isEnabled('rendering', 'characterLighting');
    if (chassisLit !== this._chassisLit) {
      this._chassisLit = chassisLit;
      setForgeChassisLighting(this.scene, chassisLit);
    }

    if (now >= this._bodyFormLODRefreshAt || this._bodyFormLODSelectedBotId !== this.selectedBotId) {
      this._bodyFormNearIDs = selectNearBodyFormIDs(
        this.entries,
        this.scene.activeCamera,
        BODY_FORM_NEAR_CHARACTER_LIMIT,
        this.selectedBotId,
      );
      this._bodyFormLODRefreshAt = now + BODY_FORM_LOD_SELECTION_INTERVAL_MS;
      this._bodyFormLODSelectedBotId = this.selectedBotId;
    }

    const cameraRadius = Number(this.scene.activeCamera?.radius) || 800;
    const hudScale = worldHudScaleForRadius(cameraRadius);
    for (const [id, entry] of this.entries) {
      if (entry.worldHud && entry._worldHudScale !== hudScale) {
        scaleWorldBotHud(entry.worldHud, cameraRadius);
        entry._worldHudScale = hudScale;
      }
      if (entry.worldHud?.tauntBubble?.isVisible &&
          (!entry.isAlive || now >= entry.tauntHideAt)) {
        hideWorldTaunt(entry.worldHud);
      }
      // A completed corpse is both hidden and immutable until the next server
      // respawn snapshot. Do not keep sampling poses for it every display frame.
      if (!entry.isAlive && entry.anim?.deathTimer >= 0.88 &&
          typeof entry.root.isEnabled === 'function' && !entry.root.isEnabled()) {
        continue;
      }
      if (!entry._interpReady) continue;
      if (entry.isAlive) {
        // Exponential smoothing toward current server position.
        const smoothing = 6;
        const factor = 1 - Math.exp(-smoothing * dt);
        const prevX = entry.root.position.x;
        const prevZ = entry.root.position.z;
        entry.root.position.x += (entry.currPos[0] - prevX) * factor;
        entry.root.position.z += (entry.currPos[1] - prevZ) * factor;

        // Face movement direction (only when actually moving, don't override attack facing)
        const vx = entry.root.position.x - prevX;
        const vz = entry.root.position.z - prevZ;
        if (vx * vx + vz * vz > 0.01 && entry.anim.attackTimer < 0) {
          entry.anim.targetRotY = Math.atan2(vx, vz);
        }
      }
      // Tick the one production character system every frame. Far bots keep
      // their state clocks current without rewriting disabled articulated joints.
      const forceFarBodyForm = entry.bodyFormKey !== 'standard' && !this._bodyFormNearIDs.has(id);
      const farLOD = updateForgeCharacterLOD(entry, this.scene.activeCamera, forceFarBodyForm);
      updateForgeCharacter(entry, dt, this._motionQuery?.matches === true, !farLOD);

      // Damage flinch: Forge leaves root scaling free for this short squash.
      if (entry._flinchT > 0) {
        entry._flinchT -= dt;
        if (entry._flinchT <= 0 || !isEnabled('hitReactions', 'damageFlinch')) {
          entry._flinchT = 0;
          entry.root.scaling.setAll(1); // release to rest
        } else {
          const k = entry._flinchT / 0.18; // 1 at impact -> 0 at rest
          entry.root.scaling.setAll(1 - 0.14 * entry._flinch * k);
        }
      }
    }
  }

  /** @private Apply visual indicators for dodge (invulnerability) and stun. */
  _updateStatusEffects(entry, bot, now = performance.now()) {
    // Dodge / invulnerability — semi-transparent. While the bot is dead the
    // death fade in the anim tick owns alpha; writing 1 here every server
    // tick keeps corpses opaque and fights that fade, so steer alpha only
    // for live bots.
    // Babylon instances do not support per-instance `visibility`; assigning it
    // only emits a warning and has no visual effect. Keep the shared structural
    // silhouette opaque and apply dodge feedback to the bot-owned accent/core
    // materials. This also prevents a distant dodge from erasing the far proxy.
    if (bot.is_alive) {
      const alpha = bot.is_dodging ? 0.5 : 1;
      if (entry._forgeStatusAlpha !== alpha) {
        forEachForgeStatusMaterial(entry, material => { material.alpha = alpha; });
        entry._forgeStatusAlpha = alpha;
      }
    }

    // Stun — red emissive tint (transient, wins over the wounded look)
    if (bot.is_alive && bot.is_stunned) {
      forEachForgeStatusMaterial(entry, material => {
        material.emissiveColor.set(0.8, 0.15, 0.1);
      });
    } else if (bot.is_alive || entry._stunActive || entry._woundedActive) {
      // Resting emissive. _updateStatusEffects is the single owner of the
      // non-stunned baseline, so the wounded look layers in here rather than
      // fighting the attack-glow the anim tick adds on top. Below 35% HP the
      // body dims (a fading, failing look); below 15% a slow red heartbeat
      // pulses in. When a bot dies while stunned/wounded, restore the baseline
      // before _deathFlash captures the material's original color.
      const hpRatio = bot.is_alive && bot.max_hp > 0 ? bot.hp / bot.max_hp : 1;
      const tintEnabled = isEnabled('hitReactions', 'woundedTint');
      const wounded = tintEnabled && bot.is_alive && hpRatio < 0.35;
      const critical = tintEnabled && bot.is_alive && hpRatio < 0.15;
      if (wounded || entry._stunActive || entry._woundedActive) {
        const dim = wounded ? 0.6 : 1;
        // ~1.2Hz sine from the wall clock (0..1), scaled into a red add.
        const redBoost = critical ? (Math.sin(now * 0.00754) * 0.5 + 0.5) * 0.5 : 0;
        forEachForgeStatusMaterial(entry, (material, index) => {
          const resting = material._forgeRestEmissive;
          const diffuse = material.diffuseColor;
          const baseR = resting?.r ?? diffuse.r * (index === 0 ? 0.35 : 0.4);
          const baseG = resting?.g ?? diffuse.g * (index === 0 ? 0.35 : 0.4);
          const baseB = resting?.b ?? diffuse.b * (index === 0 ? 0.35 : 0.4);
          material.emissiveColor.set(
            Math.min(baseR * dim + redBoost, 1), baseG * dim, baseB * dim);
        });
      }
      entry._woundedActive = wounded || critical;
    }
    entry._stunActive = bot.is_alive && !!bot.is_stunned;
  }

  /** @private Flash body white on death using Babylon.js Animation API. */
  _deathFlash(entry) {
    const B = window.BABYLON;
    forEachForgeStatusMaterial(entry, (material, index) => {
      const original = material.emissiveColor.clone();
      const animation = new B.Animation(`deathFlash-${index}`, 'emissiveColor', 100,
        B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
      animation.setKeys([
        { frame: 0, value: new B.Color3(1, 1, 1) },
        { frame: 30, value: original.clone() }
      ]);
      this.scene.beginDirectAnimation(material, [animation], 0, 30, false);
    });
  }

  /**
   * Shows a short-lived speech bubble above a bot. Text is server-authored
   * (the taunt emote table) and is canvas-drawn into a small world texture.
   * One bubble per bot; a new taunt replaces it.
   */
  showTaunt(botId, text) {
    if (!isEnabled('taunts', 'speechBubbles')) return;
    const entry = this.entries.get(botId);
    if (!entry || !entry.isAlive) return;
    showWorldTaunt(entry.worldHud, text);
    // Wall-clock expiry swept in interpolate(): survives tab throttling and
    // needs no timers that could outlive a scene rebuild.
    entry.tauntHideAt = performance.now() + 2600;
  }

  playImpactReaction(botId, fromX, fromZ) {
    const entry = this.entries.get(botId);
    if (!entry || !entry.bodyMat || !entry.headMat) return;
    // Stamp the hit source (scalars only) so the damage flinch on the next
    // HP tick can recoil directionally away from the attacker. This bookkeeping
    // feeds the separate `hitReactions.damageFlinch` toggle, so it must run
    // even when the impact flash below is disabled.
    if (typeof fromX === 'number' && typeof fromZ === 'number') {
      entry._hitFromX = fromX;
      entry._hitFromZ = fromZ;
      entry._hitFromT = performance.now();
    }
    // Event-driven kill paths (bow, spear, shield, backstab, grapple) call
    // this unguarded on the tick after death; the stamp above still feeds the
    // directional fall, but the flash/squash below must not touch a corpse
    // (it would white-flash and root-squash the death choreography).
    if (!entry.isAlive) return;
    if (!isEnabled('hitReactions', 'impactFlash')) return;
    const B = window.BABYLON;
    forEachForgeStatusMaterial(entry, (material, index) => {
      const original = material.emissiveColor.clone();
      const animation = new B.Animation(`impactFlash-${index}`, 'emissiveColor', 100,
        B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
      animation.setKeys([
        { frame: 0, value: new B.Color3(1, 1, 1) },
        { frame: 10, value: original.clone() }
      ]);
      this.scene.beginDirectAnimation(material, [animation], 0, 10, false);
    });

    // The Forge rig authors part dimensions in `scaling` (torso ~7x8x4), so
    // the legacy absolute 1.08 -> 1,1,1 squash keys collapsed the torso and
    // head to unit-sized specks on the FIRST ranged hit and left them that
    // way for the rest of the session (bots looked half-missing once the
    // zone forced fights). Squash relative to each part's authored scale,
    // captured once so overlapping impacts cannot compound the shrink.
    if (!entry._impactScaleBase) {
      entry._impactScaleBase = {
        body: entry.body.scaling.clone(),
        head: entry.head.scaling.clone(),
      };
    }
    for (const [mesh, base] of [
      [entry.body, entry._impactScaleBase.body],
      [entry.head, entry._impactScaleBase.head],
    ]) {
      const squash = new B.Animation('impactSquash', 'scaling', 100,
        B.Animation.ANIMATIONTYPE_VECTOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
      squash.setKeys([
        { frame: 0, value: new B.Vector3(base.x * 1.08, base.y * 0.92, base.z * 1.08) },
        { frame: 10, value: base.clone() }
      ]);
      this.scene.beginDirectAnimation(mesh, [squash], 0, 10, false);
    }
  }

  handlePick(mesh) {
    const botId = mesh?.metadata?.botId;
    if (!botId) {
      this.clearSelection();
      return false;
    }
    this.selectBot(this.selectedBotId === botId ? null : botId);
    return true;
  }

  selectBot(botId) {
    if (this.selectedBotId) {
      hideWorldSelection(this.entries.get(this.selectedBotId)?.worldHud);
    }
    this.selectedBotId = botId || null;
    this._refreshSelectionPanel();
    if (this.onSelectionChange) this.onSelectionChange(this.selectedBotId);
  }

  clearSelection() {
    this.selectBot(null);
  }

  _refreshSelectionPanel() {
    if (!this.selectedBotId) return;
    const entry = this.entries.get(this.selectedBotId);
    if (!entry || !entry.botData || !entry.root) {
      return;
    }
    const bot = entry.botData;
    const lines = [
      `${bot.name || 'Unknown'}${bot.is_bounty_target ? ' [BOUNTY]' : ''}`,
      `HP ${bot.hp}/${bot.max_hp}  ${bot.weapon || 'unknown'}`,
      `Kills ${bot.round_kills || 0}  Streak ${bot.kill_streak || 0}`,
      `Shield ${bot.shield_absorb || 0}  CD ${bot.cooldown_remaining || 0}s`,
      `Mines ${bot.mine_count || 0}  Grapple ${bot.grapple_charges || 0}`,
    ];
    if (bot.is_alive) showWorldSelection(entry.worldHud, lines);
    else hideWorldSelection(entry.worldHud);
  }
}
