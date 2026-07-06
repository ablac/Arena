'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
 * Detects attacks from spectator state and triggers weapon swing + hit effects.
 * @module renderer/bots
 */

import { createBotEntry, disposeBotEntry, getGuiTexture, setHpColor } from './bot-body.js?v=20260706f';
import { updateBotAnim, triggerAttack, triggerDodge, triggerShove, meleeContactDelay } from './animations.js?v=20260706f';
import { updateSwordsmanAnim, triggerSwordsmanAttack, triggerSwordsmanDodge, updateSwordsmanStance, triggerSwordsmanHit } from './swordsman-anims.js?v=20260706f';
import { isEnabled } from '../settings.js';

export class BotRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.entries = new Map();
    this._lastFrame = performance.now();
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
    this._initSelectionPanel();
  }

  _initSelectionPanel() {
    const GUI = window.BABYLON.GUI;
    const adt = getGuiTexture();
    const panel = new GUI.Rectangle('bot-summary-panel');
    panel.width = '220px';
    panel.height = '122px';
    panel.thickness = 1;
    panel.cornerRadius = 12;
    panel.color = '#8adfff';
    panel.background = 'rgba(8,12,20,0.9)';
    panel.isVisible = false;
    adt.addControl(panel);
    this.summaryPanel = panel;

    const text = new GUI.TextBlock('bot-summary-text');
    text.paddingLeft = '10px';
    text.paddingRight = '10px';
    text.paddingTop = '8px';
    text.paddingBottom = '8px';
    text.textWrapping = true;
    text.textHorizontalAlignment = GUI.Control.HORIZONTAL_ALIGNMENT_LEFT;
    text.textVerticalAlignment = GUI.Control.VERTICAL_ALIGNMENT_TOP;
    text.color = 'white';
    text.fontFamily = 'monospace';
    text.fontSize = 12;
    text.lineSpacing = '2px';
    panel.addControl(text);
    this.summaryText = text;
  }

  update(bots) {
    const seen = new Set();

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

      if (!entry) {
        entry = createBotEntry(bot, this.scene);
        this.entries.set(bot.bot_id, entry);
      }
      entry.botData = bot;

      // Entity interpolation: store last two server positions + timing.
      const now = performance.now();
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
        entry.prevPos = [entry.currPos[0], entry.currPos[1]];
        entry.currPos = [bot.position[0], bot.position[1]];
        // Measure actual interval between server updates for accurate lerp.
        const elapsed = now - entry._interpStart;
        if (elapsed > 30) entry._interpDur = elapsed;
        entry._interpStart = now;
      }

      // Visibility
      entry.root.setEnabled(bot.is_alive);
      if (entry.nameLabel) entry.nameLabel.isVisible = bot.is_alive;
      if (entry.hpContainer) entry.hpContainer.isVisible = bot.is_alive;

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
          const fresh = entry._hitFromT && (performance.now() - entry._hitFromT) < 400;
          entry._hitYaw = fresh
            ? Math.atan2(entry._hitFromX - bot.position[0], entry._hitFromZ - bot.position[1])
            : entry.root.rotation.y;
          if (entry.isSwordsman) {
            triggerSwordsmanHit(entry.anim, entry._hitYaw, entry._flinch);
          }
        }
        entry._lastHp = bot.hp;
        const hpRatio = bot.hp / bot.max_hp;
        entry.hpFill.width = Math.max(0.01, hpRatio);
        setHpColor(entry.hpFill, hpRatio);
        // Wound level drives the idle slump in updateBotAnim: 0 healthy,
        // 1 wounded (<35%), 2 critical (<15%). Recomputed on every HP change,
        // so a heal or respawn (HP back up) clears it automatically.
        if (entry.anim) {
          entry.anim.woundLevel = hpRatio < 0.15 ? 2 : (hpRatio < 0.35 ? 1 : 0);
        }
      }

      // Status effect visuals (dodge transparency, stun tint)
      this._updateStatusEffects(entry, bot);

      // Attack detection BEFORE animation so triggerAttack takes effect this frame
      const weaponType = bot.weapon || 'sword';
      const botAction = bot.action || bot.last_action; // server sends last_action
      const liveCooldown = Number(bot.cooldown_remaining || 0);
      const prevCooldown = Number(entry._lastCooldown ?? 0);
      const attackJustStarted =
        botAction === 'attack' &&
        bot.is_alive &&
        entry._wasAlive &&
        (
          liveCooldown > prevCooldown + 0.35 ||
          (entry._lastAction !== 'attack' && liveCooldown > 0.05)
        );

      if (attackJustStarted) {
        if (entry.isSwordsman) {
          triggerSwordsmanAttack(entry.anim, liveCooldown);
        } else {
          triggerAttack(entry.anim, weaponType, liveCooldown);
        }

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
            const dur = liveCooldown > 0.16 ? liveCooldown : 0.5;
            const contactDelay = entry.isSwordsman
              ? 0.55 * dur // contact keyframe t in the swordsman choreography
              : meleeContactDelay(weaponType, liveCooldown);
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
      if (botAction === 'shove' && bot.is_alive && entry._wasAlive) {
        // The generic shove drives the WEAPON_ANIMS phase machine, which the
        // swordsman rig does not run: on a SwordsmanAnimState it sets
        // attackTimer=0 with no keyframes, wedging the timer at 0 and gating
        // all future swings. Swordsmen keep the facing turn and shove ring
        // but skip the generic trigger.
        if (!entry.isSwordsman) triggerShove(entry.anim);

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

      // Grapple detection
      if (botAction === 'grapple' && bot.is_alive && entry._wasAlive) {
        const targetPos = bot.target_id ? getPosMap().get(bot.target_id) : null;
        if (targetPos && this.onGrapple) {
          this.onGrapple(bot.position[0], bot.position[1], targetPos[0], targetPos[1]);
        }
      }

      // Dodge detection
      if (botAction === 'dodge' && bot.is_alive && entry._wasAlive) {
        if (entry.isSwordsman) {
          triggerSwordsmanDodge(entry.anim, entry.anim.moveAngle);
        } else {
          triggerDodge(entry.anim, entry.anim.moveAngle);
        }
        if (this.onDodge) {
          this.onDodge(bot.position[0], bot.position[1], bot.avatar_color);
        }
      }

      // Swordsman stance update based on HP
      if (entry.isSwordsman && bot.hp != null && bot.max_hp > 0) {
        updateSwordsmanStance(entry.anim, bot.hp / bot.max_hp);
      }

      // Death flash
      if (!bot.is_alive && entry._wasAlive) {
        if (isEnabled('deathEffects', 'deathFlash')) this._deathFlash(entry);
        // Release the flinch channel so a respawn starts at rest scale,
        // and zero the hit-reaction rotations so a respawn stands straight.
        entry._flinchT = 0;
        entry.root.scaling.setAll(1);
        if (!entry.isSwordsman) {
          if (entry.head) { entry.head.rotation.x = 0; entry.head.rotation.z = 0; }
          if (entry.body) { entry.body.rotation.x = 0; entry.body.rotation.z = 0; }
        }
      }
      entry._wasAlive = bot.is_alive;
      entry.isAlive = bot.is_alive;
      entry._lastCooldown = liveCooldown;
      entry._lastAction = botAction;
    }

    for (const [id, entry] of this.entries) {
      if (!seen.has(id)) {
        if (this.selectedBotId === id) this.clearSelection();
        disposeBotEntry(entry);
        this.entries.delete(id);
      }
    }

    this._refreshSelectionPanel();
  }

  /**
   * Called every render frame — linearly interpolates bot positions between
   * the last two server snapshots at constant speed, then ticks animations.
   */
  interpolate() {
    const now = performance.now();
    const dt = Math.min((now - this._lastFrame) / 1000, 0.1);
    this._lastFrame = now;

    for (const [, entry] of this.entries) {
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
      // Tick animations every frame for smooth playback
      if (entry.isSwordsman) {
        updateSwordsmanAnim(entry, dt);
      } else {
        updateBotAnim(
          entry.anim, entry.root, entry.weapon,
          entry.root.position.x, entry.root.position.z,
          entry.isAlive, dt, entry.bodyMat, entry
        );
      }

      // Damage flinch: a quick squash on the root, decaying to rest. The root
      // scale is free MOST of the time, but updateBotAnim (which takes entry.root
      // as its body param) does drive root scaling during dodge, death, and
      // respawn; swordsmen never touch the root at all. So write the flinch only
      // when that shared channel is otherwise free, and yield to the anim when it
      // owns the scale. This keeps the two from fighting and stops any residual
      // from compounding. Runs after the anim tick so a free-channel write wins.
      if (entry._flinchT > 0) {
        entry._flinchT -= dt;
        const a = entry.anim;
        const animOwnsScale = !entry.isSwordsman && a &&
          (a.dodgeTimer >= 0 || a.deathTimer >= 0 || a.respawnTimer >= 0);
        if (animOwnsScale) {
          // Anim is driving root scaling this frame; the flinch sits it out.
          // It resumes on a later free frame if any time is left.
        } else if (entry._flinchT <= 0 || !isEnabled('hitReactions', 'damageFlinch')) {
          entry._flinchT = 0;
          entry.root.scaling.setAll(1); // release to rest
          if (!entry.isSwordsman) {
            // Rest the directional reaction channels exactly at zero.
            if (entry.head) { entry.head.rotation.x = 0; entry.head.rotation.z = 0; }
            if (entry.body) { entry.body.rotation.x = 0; entry.body.rotation.z = 0; }
          }
        } else {
          const k = entry._flinchT / 0.18; // 1 at impact -> 0 at rest
          entry.root.scaling.setAll(1 - 0.14 * entry._flinch * k);
          if (!entry.isSwordsman && entry.head && entry.body) {
            // Directional head snap + torso lean AWAY from the attacker.
            // head and this body child mesh are uncontended channels
            // (updateBotAnim poses entry.root, never these two). Two trig
            // calls per flinching bot, zero allocations.
            const rel = (entry._hitYaw ?? entry.root.rotation.y) - entry.root.rotation.y;
            const amp = entry._flinch * k;
            entry.head.rotation.x = -0.5 * amp * Math.cos(rel);
            entry.head.rotation.z = 0.35 * amp * Math.sin(rel);
            entry.body.rotation.x = -0.25 * amp * Math.cos(rel);
            entry.body.rotation.z = 0.18 * amp * Math.sin(rel);
          }
        }
      }
    }
  }

  /** @private Apply visual indicators for dodge (invulnerability) and stun. */
  _updateStatusEffects(entry, bot) {
    // Dodge / invulnerability — semi-transparent. While the bot is dead the
    // death fade in the anim tick owns alpha; writing 1 here every server
    // tick keeps corpses opaque and fights that fade, so steer alpha only
    // for live bots.
    if (bot.is_dodging) {
      entry.bodyMat.alpha = 0.5;
      entry.headMat.alpha = 0.5;
    } else if (bot.is_alive) {
      entry.bodyMat.alpha = 1;
      entry.headMat.alpha = 1;
    }

    // Stun — red emissive tint (transient, wins over the wounded look)
    if (bot.is_alive && bot.is_stunned) {
      entry.bodyMat.emissiveColor.set(0.8, 0.15, 0.1);
      entry.headMat.emissiveColor.set(0.8, 0.15, 0.1);
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
        const redBoost = critical ? (Math.sin(performance.now() * 0.00754) * 0.5 + 0.5) * 0.5 : 0;
        const bc = entry.bodyMat.diffuseColor;
        entry.bodyMat.emissiveColor.set(
          Math.min(bc.r * 0.35 * dim + redBoost, 1), bc.g * 0.35 * dim, bc.b * 0.35 * dim);
        const hc = entry.headMat.diffuseColor;
        entry.headMat.emissiveColor.set(
          Math.min(hc.r * 0.4 * dim + redBoost, 1), hc.g * 0.4 * dim, hc.b * 0.4 * dim);
      }
      entry._woundedActive = wounded || critical;
    }
    entry._stunActive = bot.is_alive && !!bot.is_stunned;
  }

  /** @private Flash body white on death using Babylon.js Animation API. */
  _deathFlash(entry) {
    const B = window.BABYLON;
    const origColor = entry.bodyMat.emissiveColor.clone();

    const bodyAnim = new B.Animation('deathFlashBody', 'emissiveColor', 100,
      B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    bodyAnim.setKeys([
      { frame: 0, value: new B.Color3(1, 1, 1) },
      { frame: 30, value: origColor.clone() }
    ]);

    const headAnim = new B.Animation('deathFlashHead', 'emissiveColor', 100,
      B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    headAnim.setKeys([
      { frame: 0, value: new B.Color3(1, 1, 1) },
      { frame: 30, value: origColor.clone() }
    ]);

    this.scene.beginDirectAnimation(entry.bodyMat, [bodyAnim], 0, 30, false);
    this.scene.beginDirectAnimation(entry.headMat, [headAnim], 0, 30, false);
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
    if (!isEnabled('hitReactions', 'impactFlash')) return;
    const B = window.BABYLON;
    const bodyOrig = entry.bodyMat.emissiveColor.clone();
    const headOrig = entry.headMat.emissiveColor.clone();

    const bodyAnim = new B.Animation('bowHitBody', 'emissiveColor', 100,
      B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    bodyAnim.setKeys([
      { frame: 0, value: new B.Color3(1, 1, 1) },
      { frame: 10, value: bodyOrig.clone() }
    ]);

    const headAnim = new B.Animation('bowHitHead', 'emissiveColor', 100,
      B.Animation.ANIMATIONTYPE_COLOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    headAnim.setKeys([
      { frame: 0, value: new B.Color3(1, 1, 1) },
      { frame: 10, value: headOrig.clone() }
    ]);

    const scaleAnim = new B.Animation('bowHitScale', 'scaling', 100,
      B.Animation.ANIMATIONTYPE_VECTOR3, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    scaleAnim.setKeys([
      { frame: 0, value: new B.Vector3(1.08, 0.92, 1.08) },
      { frame: 10, value: new B.Vector3(1, 1, 1) }
    ]);

    this.scene.beginDirectAnimation(entry.bodyMat, [bodyAnim], 0, 10, false);
    this.scene.beginDirectAnimation(entry.headMat, [headAnim], 0, 10, false);
    this.scene.beginDirectAnimation(entry.body, [scaleAnim], 0, 10, false);
    this.scene.beginDirectAnimation(entry.head, [scaleAnim], 0, 10, false);
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
    this.selectedBotId = botId || null;
    this._refreshSelectionPanel();
    if (this.onSelectionChange) this.onSelectionChange(this.selectedBotId);
  }

  clearSelection() {
    this.selectBot(null);
  }

  _refreshSelectionPanel() {
    if (!this.summaryPanel || !this.summaryText) return;
    if (!this.selectedBotId) {
      this.summaryPanel.isVisible = false;
      this.summaryPanel.linkWithMesh(null);
      return;
    }
    const entry = this.entries.get(this.selectedBotId);
    if (!entry || !entry.botData || !entry.root) {
      this.summaryPanel.isVisible = false;
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
    this.summaryText.text = lines.join('\n');
    this.summaryPanel.linkWithMesh(entry.root);
    this.summaryPanel.linkOffsetY = -128;
    this.summaryPanel.isVisible = !!bot.is_alive;
  }
}
