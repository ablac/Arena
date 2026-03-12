'use strict';

/**
 * Bot renderer — manages 3D humanoid bot entities with animations.
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
  }

  /**
   * Update bots from arena state.
   * @param {Array} bots
   */
  update(bots) {
    const seen = new Set();
    const now = performance.now();
    const dt = Math.min((now - this._lastFrame) / 1000, 0.1);
    this._lastFrame = now;

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

      const x = bot.position[0], z = bot.position[1];
      entry.root.position.x = x;
      entry.root.position.z = z;

      // Visibility
      this._setVisible(entry, bot.is_alive);

      // HP bar
      const hpRatio = bot.hp / bot.max_hp;
      entry.hpBar.scaling.x = Math.max(0.01, hpRatio);
      entry.hpBar.position.x = -HP_BAR_W * (1 - hpRatio) / 2;
      setHpColor(entry.hpMat, hpRatio);

      // Animation
      updateBotAnim(entry.anim, entry.root, entry.weapon, x, z, bot.is_alive, dt);

      // Attack detection (simple: if action field present)
      if (bot.action === 'attack' && entry._wasAlive && bot.is_alive) {
        triggerAttack(entry.anim);
      }

      // Death flash
      if (!bot.is_alive && entry._wasAlive) {
        this._deathFlash(entry);
      }
      entry._wasAlive = bot.is_alive;
      entry.isAlive = bot.is_alive;
    }

    // Remove disconnected bots
    for (const [id, entry] of this.entries) {
      if (!seen.has(id)) {
        disposeBotEntry(entry);
        this.entries.delete(id);
      }
    }
  }

  /**
   * Interpolate positions (called from render loop at 60fps).
   * @param {number} alpha 0..1
   */
  interpolate(alpha) {
    for (const [, entry] of this.entries) {
      if (!entry.isAlive || !entry.prevPos || !entry.currPos) continue;
      const x = entry.prevPos[0] + (entry.currPos[0] - entry.prevPos[0]) * alpha;
      const z = entry.prevPos[1] + (entry.currPos[1] - entry.prevPos[1]) * alpha;
      entry.root.position.x = x;
      entry.root.position.z = z;
    }
  }

  /** @private Toggle all meshes in entry. */
  _setVisible(entry, visible) {
    entry.root.setEnabled(visible);
  }

  /** @private Flash body white on death. */
  _deathFlash(entry) {
    const B = window.BABYLON;
    const orig = entry.bodyMat.emissiveColor.clone();
    entry.bodyMat.emissiveColor = new B.Color3(1, 1, 1);
    entry.headMat.emissiveColor = new B.Color3(1, 1, 1);
    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = Math.min((performance.now() - start) / 300, 1);
      const fade = 1 - t;
      entry.bodyMat.emissiveColor = B.Color3.Lerp(orig, new B.Color3(1, 1, 1), fade);
      entry.headMat.emissiveColor = entry.bodyMat.emissiveColor.clone();
      if (t >= 1) this.scene.onBeforeRenderObservable.remove(obs);
    });
  }
}
