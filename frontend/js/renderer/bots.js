'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
 * Detects attacks from spectator state and triggers weapon swing + hit effects.
 * @module renderer/bots
 */

import { createBotEntry, disposeBotEntry, getGuiTexture, setHpColor } from './bot-body.js?v=20260521f';
import { updateBotAnim, triggerAttack, triggerDodge, triggerShove } from './animations.js?v=20260521f';
import { updateSwordsmanAnim, triggerSwordsmanAttack, triggerSwordsmanDodge, updateSwordsmanStance } from './swordsman-anims.js';

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
        }
        entry._lastHp = bot.hp;
        const hpRatio = bot.hp / bot.max_hp;
        entry.hpFill.width = Math.max(0.01, hpRatio);
        setHpColor(entry.hpFill, hpRatio);
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
          // Notify effects system with weapon type
          if (this.onAttack) {
            this.onAttack(bot.position[0], bot.position[1],
                          targetPos[0], targetPos[1], bot.avatar_color, weaponType, {
                            targetId: bot.target_id || null,
                            targetPosition: explicitTargetPos,
                          });
          }
        }
      }

      // Shove detection
      if (botAction === 'shove' && bot.is_alive && entry._wasAlive) {
        triggerShove(entry.anim);

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
        this._deathFlash(entry);
        // Release the flinch channel so a respawn starts at rest scale.
        entry._flinchT = 0;
        entry.root.scaling.setAll(1);
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
          entry.isAlive, dt, entry.bodyMat
        );
      }

      // Damage flinch: a quick squash on the root, decaying to rest. The root
      // scale is a channel the anim tick above never writes, so this layers on
      // top of the body's own bounce/attack without fighting it. Runs after the
      // anim tick so it wins the frame; releases the channel exactly at rest.
      if (entry._flinchT > 0) {
        entry._flinchT -= dt;
        if (entry._flinchT <= 0) {
          entry._flinchT = 0;
          entry.root.scaling.setAll(1);
        } else {
          const k = entry._flinchT / 0.18; // 1 at impact -> 0 at rest
          entry.root.scaling.setAll(1 - 0.14 * entry._flinch * k);
        }
      }
    }
  }

  /** @private Apply visual indicators for dodge (invulnerability) and stun. */
  _updateStatusEffects(entry, bot) {
    // Dodge / invulnerability — semi-transparent
    if (bot.is_dodging) {
      entry.bodyMat.alpha = 0.5;
      entry.headMat.alpha = 0.5;
    } else {
      entry.bodyMat.alpha = 1;
      entry.headMat.alpha = 1;
    }

    // Stun — red emissive tint
    if (bot.is_stunned) {
      entry.bodyMat.emissiveColor.set(0.8, 0.15, 0.1);
      entry.headMat.emissiveColor.set(0.8, 0.15, 0.1);
    } else if (entry._stunActive) {
      // Restore original emissive from diffuse * emissiveFactor
      const bc = entry.bodyMat.diffuseColor;
      entry.bodyMat.emissiveColor.set(bc.r * 0.35, bc.g * 0.35, bc.b * 0.35);
      const hc = entry.headMat.diffuseColor;
      entry.headMat.emissiveColor.set(hc.r * 0.4, hc.g * 0.4, hc.b * 0.4);
    }
    entry._stunActive = !!bot.is_stunned;
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

  playImpactReaction(botId) {
    const entry = this.entries.get(botId);
    if (!entry || !entry.bodyMat || !entry.headMat) return;
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
