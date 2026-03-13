'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
 * Detects attacks from spectator state and triggers weapon swing + hit effects.
 * @module renderer/bots
 */

import { createBotEntry, disposeBotEntry, setHpColor } from './bot-body.js';
import { updateBotAnim, triggerAttack } from './animations.js';

const HP_BAR_W = 40;

export class BotRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.entries = new Map();
    this._lastFrame = performance.now();
    /** @type {Function|null} callback(attackerX, attackerZ, targetX, targetZ, color) */
    this.onAttack = null;
  }

  update(bots) {
    const seen = new Set();
    const now = performance.now();
    const dt = Math.min((now - this._lastFrame) / 1000, 0.1);
    this._lastFrame = now;

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

      // Interpolation positions
      entry.prevPos = entry.currPos
        ? [entry.currPos[0], entry.currPos[1]]
        : [bot.position[0], bot.position[1]];
      entry.currPos = [bot.position[0], bot.position[1]];

      entry.root.position.x = bot.position[0];
      entry.root.position.z = bot.position[1];

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

      // Animation
      updateBotAnim(entry.anim, entry.root, entry.weapon, bot.position[0], bot.position[1], bot.is_alive, dt);

      // Attack detection from server state
      if (bot.action === 'attack' && bot.is_alive && entry._wasAlive) {
        triggerAttack(entry.anim);

        // Face toward target (smoothed via anim.targetRotY)
        const targetPos = bot.target_id ? getPosMap().get(bot.target_id) : null;
        if (targetPos) {
          const adx = targetPos[0] - bot.position[0];
          const adz = targetPos[1] - bot.position[1];
          if (adx !== 0 || adz !== 0) {
            entry.anim.targetRotY = Math.atan2(adx, adz);
          }
          // Notify effects system
          if (this.onAttack) {
            this.onAttack(bot.position[0], bot.position[1],
                          targetPos[0], targetPos[1], bot.avatar_color);
          }
        }
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

  interpolate(alpha) {
    for (const [, entry] of this.entries) {
      if (!entry.isAlive || !entry.prevPos || !entry.currPos) continue;
      entry.root.position.x = entry.prevPos[0] + (entry.currPos[0] - entry.prevPos[0]) * alpha;
      entry.root.position.z = entry.prevPos[1] + (entry.currPos[1] - entry.prevPos[1]) * alpha;
    }
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
