'use strict';

/**
 * Visual effects — death bursts using Babylon particle system.
 * Sprite-ready: effects can be swapped for sprite-based animations.
 * @module renderer/effects
 */

export class EffectRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Set<string>} Previously alive bot IDs */
    this.previouslyAlive = new Set();
    /** @type {Array<{system: BABYLON.ParticleSystem, created: number}>} */
    this.activeEffects = [];
  }

  /**
   * Detect deaths and spawn burst effects.
   * @param {Array} bots - Bot array from arena state
   */
  update(bots) {
    const now = Date.now();
    const currentlyAlive = new Set();

    for (const bot of bots) {
      if (bot.is_alive) {
        currentlyAlive.add(bot.bot_id);
      } else if (this.previouslyAlive.has(bot.bot_id)) {
        // Bot just died — spawn death effect
        this._spawnDeathBurst(bot.position[0], bot.position[1], bot.avatar_color);
      }
    }

    this.previouslyAlive = currentlyAlive;
    this._cleanupEffects(now);
  }

  /**
   * Spawn a particle burst at a position.
   * @param {number} x
   * @param {number} z
   * @param {string} hexColor
   */
  _spawnDeathBurst(x, z, hexColor) {
    const BABYLON = window.BABYLON;
    const emitter = new BABYLON.Vector3(x, 2, z);

    const ps = new BABYLON.ParticleSystem(`death-${Date.now()}`, 50, this.scene);
    ps.emitter = emitter;
    ps.createPointEmitter(
      new BABYLON.Vector3(-1, 1, -1),
      new BABYLON.Vector3(1, 1, 1)
    );

    const color = this._parseColor(hexColor);
    ps.color1 = new BABYLON.Color4(color.r, color.g, color.b, 1);
    ps.color2 = new BABYLON.Color4(1, 0.3, 0.1, 1);
    ps.colorDead = new BABYLON.Color4(0.2, 0.2, 0.2, 0);

    ps.minSize = 2;
    ps.maxSize = 5;
    ps.minLifeTime = 0.3;
    ps.maxLifeTime = 0.8;
    ps.emitRate = 200;
    ps.minEmitPower = 20;
    ps.maxEmitPower = 60;
    ps.gravity = new BABYLON.Vector3(0, -30, 0);
    ps.blendMode = BABYLON.ParticleSystem.BLENDMODE_ADD;

    ps.targetStopDuration = 0.15;
    ps.disposeOnStop = true;
    ps.start();

    this.activeEffects.push({ system: ps, created: Date.now() });
  }

  /** @private Remove old effects. */
  _cleanupEffects(now) {
    this.activeEffects = this.activeEffects.filter(e => {
      if (now - e.created > 2000) {
        if (!e.system.isDisposed) e.system.dispose();
        return false;
      }
      return true;
    });
  }

  /** @private Parse hex color to BABYLON.Color3. */
  _parseColor(hex) {
    const BABYLON = window.BABYLON;
    if (!hex || hex.length < 7) return new BABYLON.Color3(1, 0.5, 0.3);
    const r = parseInt(hex.slice(1, 3), 16) / 255;
    const g = parseInt(hex.slice(3, 5), 16) / 255;
    const b = parseInt(hex.slice(5, 7), 16) / 255;
    return new BABYLON.Color3(r, g, b);
  }
}
