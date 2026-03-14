'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
 * Detects attacks from spectator state and triggers weapon swing + hit effects.
 * @module renderer/bots
 */

import { createBotEntry, disposeBotEntry, setHpColor } from './bot-body.js';
import { updateBotAnim, triggerAttack, triggerDodge, triggerShove } from './animations.js';
import { updateSwordsmanAnim, triggerSwordsmanAttack, triggerSwordsmanDodge, updateSwordsmanStance } from './swordsman-anims.js';

const HP_BAR_W = 40;

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

      // HP bar — only update when HP changes
      if (bot.hp !== entry._lastHp) {
        entry._lastHp = bot.hp;
        const hpRatio = bot.hp / bot.max_hp;
        entry.hpBar.scaling.x = Math.max(0.01, hpRatio);
        entry.hpBar.position.x = -HP_BAR_W * (1 - hpRatio) / 2;
        setHpColor(entry.hpMat, hpRatio);
      }

      // Status effect visuals (dodge transparency, stun tint)
      this._updateStatusEffects(entry, bot);

      // Attack detection BEFORE animation so triggerAttack takes effect this frame
      const weaponType = bot.weapon || 'sword';
      const botAction = bot.action || bot.last_action; // server sends last_action
      if (botAction === 'attack' && bot.is_alive && entry._wasAlive) {
        if (entry.isSwordsman) {
          triggerSwordsmanAttack(entry.anim);
        } else {
          triggerAttack(entry.anim, weaponType);
        }

        // Face toward target (smoothed via anim.targetRotY)
        const targetPos = bot.target_id ? getPosMap().get(bot.target_id) : null;
        if (targetPos) {
          const adx = targetPos[0] - bot.position[0];
          const adz = targetPos[1] - bot.position[1];
          if (adx !== 0 || adz !== 0) {
            entry.anim.targetRotY = Math.atan2(adx, adz);
          }
          // Notify effects system with weapon type
          if (this.onAttack) {
            this.onAttack(bot.position[0], bot.position[1],
                          targetPos[0], targetPos[1], bot.avatar_color, weaponType);
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
      }
      entry._wasAlive = bot.is_alive;
      entry.isAlive = bot.is_alive;
    }

    for (const [id, entry] of this.entries) {
      if (!seen.has(id)) {
        disposeBotEntry(entry);
        this.entries.delete(id);
      }
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

    for (const [, entry] of this.entries) {
      if (!entry._interpReady) continue;
      if (entry.isAlive) {
        // Linear interpolation at constant speed between prevPos → currPos.
        // t = 0..1 maps to prevPos..currPos; clamp at 1 so bot holds until next update.
        const t = Math.min((now - entry._interpStart) / entry._interpDur, 1);
        entry.root.position.x = entry.prevPos[0] + (entry.currPos[0] - entry.prevPos[0]) * t;
        entry.root.position.z = entry.prevPos[1] + (entry.currPos[1] - entry.prevPos[1]) * t;
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

  /** @private Flash body white on death — minimal allocations. */
  _deathFlash(entry) {
    const origR = entry.bodyMat.emissiveColor.r;
    const origG = entry.bodyMat.emissiveColor.g;
    const origB = entry.bodyMat.emissiveColor.b;
    entry.bodyMat.emissiveColor.set(1, 1, 1);
    entry.headMat.emissiveColor.set(1, 1, 1);
    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = Math.min((performance.now() - start) / 300, 1);
      const r = origR + (1 - origR) * (1 - t);
      const g = origG + (1 - origG) * (1 - t);
      const b = origB + (1 - origB) * (1 - t);
      entry.bodyMat.emissiveColor.set(r, g, b);
      entry.headMat.emissiveColor.set(r, g, b);
      if (t >= 1) this.scene.onBeforeRenderObservable.remove(obs);
    });
  }
}
