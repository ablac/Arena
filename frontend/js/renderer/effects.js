'use strict';

/**
 * Combat effects — death bursts, hit sparks, damage numbers, dodge ghosts.
 * Optimized: reduced particle counts, pooled damage textures.
 * @module renderer/effects
 */

import { parseColor } from './utils.js';

/** Max concurrent damage numbers to prevent buildup. */
const MAX_DMG_NUMBERS = 12;

/** Per-weapon hit effect configs — distinct particle colors, counts, and spread. */
const HIT_EFFECTS = {
  sword:   { count: 8,  c1: [1, 0.8, 0.3],   dead: [0.3, 0.1, 0],     minSz: 1,   maxSz: 2,   minLife: 0.06, maxLife: 0.12, rate: 120, minPow: 15, maxPow: 35, d1: [-1, 1, -1],     d2: [1, 1, 1],       stop: 0.06 },
  bow:     { count: 6,  c1: [0.9, 0.95, 1],   dead: [0.4, 0.4, 0.5],   minSz: 0.5, maxSz: 1.5, minLife: 0.08, maxLife: 0.15, rate: 80,  minPow: 10, maxPow: 25, d1: [-0.5, 1, -0.5], d2: [0.5, 1, 0.5],   stop: 0.05 },
  daggers: { count: 10, c1: [1, 0.6, 0.15],   dead: [0.4, 0.15, 0],    minSz: 0.5, maxSz: 1,   minLife: 0.04, maxLife: 0.08, rate: 150, minPow: 20, maxPow: 40, d1: [-1.5, 0.5, -1.5], d2: [1.5, 1, 1.5], stop: 0.04 },
  spear:   { count: 8,  c1: [1, 0.4, 0.2],    dead: [0.4, 0.1, 0],     minSz: 1,   maxSz: 2,   minLife: 0.05, maxLife: 0.10, rate: 100, minPow: 12, maxPow: 30, d1: [-0.3, 1, -0.3], d2: [0.3, 1, 0.3],   stop: 0.05 },
  staff:   { count: 12, c1: [0.6, 0.3, 1],    dead: [0.15, 0.05, 0.3], minSz: 2,   maxSz: 4,   minLife: 0.10, maxLife: 0.20, rate: 120, minPow: 20, maxPow: 45, d1: [-1.5, 1, -1.5], d2: [1.5, 1.5, 1.5], stop: 0.08 },
  shield:  { count: 8,  c1: [0.9, 0.9, 1],    dead: [0.3, 0.3, 0.4],   minSz: 1.5, maxSz: 3,   minLife: 0.08, maxLife: 0.15, rate: 100, minPow: 8,  maxPow: 20, d1: [-2, 0.3, -2],   d2: [2, 0.5, 2],     stop: 0.06 },
};

let _psCounter = 0;

export class EffectRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Set<string>} */
    this.prevAlive = new Set();
    /** @type {Array<{dispose: Function, created: number}>} */
    this.active = [];
    this._dmgCount = 0;
  }

  update(bots) {
    const now = Date.now();
    const alive = new Set();

    for (const bot of bots) {
      if (bot.is_alive) {
        alive.add(bot.bot_id);
      } else if (this.prevAlive.has(bot.bot_id)) {
        this._deathBurst(bot.position[0], bot.position[1], bot.avatar_color);
      }
    }

    this.prevAlive = alive;
    this._cleanup(now);
  }

  /**
   * Spawn weapon-specific hit sparks at impact point.
   * @param {number} x
   * @param {number} z
   * @param {string} hexColor - attacker avatar color
   * @param {string} [weapon='sword'] - weapon type for effect selection
   */
  spawnHitSparks(x, z, hexColor, weapon) {
    const cfg = HIT_EFFECTS[weapon] || HIT_EFFECTS.sword;
    const B = window.BABYLON;
    const ps = new B.ParticleSystem(`sparks-${++_psCounter}`, cfg.count, this.scene);
    ps.emitter = new B.Vector3(x, 12, z);
    ps.createPointEmitter(
      new B.Vector3(cfg.d1[0], cfg.d1[1], cfg.d1[2]),
      new B.Vector3(cfg.d2[0], cfg.d2[1], cfg.d2[2])
    );
    ps.color1 = new B.Color4(cfg.c1[0], cfg.c1[1], cfg.c1[2], 1);
    const c = parseColor(hexColor);
    ps.color2 = new B.Color4(c.r, c.g, c.b, 1);
    ps.colorDead = new B.Color4(cfg.dead[0], cfg.dead[1], cfg.dead[2], 0);
    ps.minSize = cfg.minSz; ps.maxSize = cfg.maxSz;
    ps.minLifeTime = cfg.minLife; ps.maxLifeTime = cfg.maxLife;
    ps.emitRate = cfg.rate;
    ps.minEmitPower = cfg.minPow; ps.maxEmitPower = cfg.maxPow;
    ps.gravity = new B.Vector3(0, -50, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = cfg.stop;
    ps.disposeOnStop = true;
    ps.start();
  }

  /**
   * Spawn dodge afterimage shimmer effect.
   * @param {number} x
   * @param {number} z
   * @param {string} hexColor
   */
  spawnDodgeEffect(x, z, hexColor) {
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const ps = new B.ParticleSystem(`dodge-${++_psCounter}`, 10, this.scene);
    ps.emitter = new B.Vector3(x, 10, z);
    ps.createPointEmitter(new B.Vector3(-1, 0.5, -1), new B.Vector3(1, 0.5, 1));
    ps.color1 = new B.Color4(c.r, c.g, c.b, 0.8);
    ps.color2 = new B.Color4(1, 1, 1, 0.6);
    ps.colorDead = new B.Color4(c.r, c.g, c.b, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.1; ps.maxLifeTime = 0.3;
    ps.emitRate = 200;
    ps.minEmitPower = 10; ps.maxEmitPower = 25;
    ps.gravity = new B.Vector3(0, -10, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.05;
    ps.disposeOnStop = true;
    ps.start();
  }

  /**
   * Spawn a shove shockwave effect at the impact point (target position).
   * Creates a directional blast of particles from attacker toward target.
   * @param {number} ax - attacker x
   * @param {number} az - attacker z
   * @param {number} tx - target x
   * @param {number} tz - target z
   * @param {string} hexColor - attacker avatar color
   */
  spawnShoveEffect(ax, az, tx, tz, hexColor) {
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    // Direction from attacker to target
    const dx = tx - ax;
    const dz = tz - az;
    const len = Math.sqrt(dx * dx + dz * dz) || 1;
    const nx = dx / len;
    const nz = dz / len;

    const ps = new B.ParticleSystem(`shove-${++_psCounter}`, 15, this.scene);
    ps.emitter = new B.Vector3(tx, 10, tz);
    // Blast outward in the push direction
    ps.createPointEmitter(
      new B.Vector3(nx * 0.5 - 0.3, 0.3, nz * 0.5 - 0.3),
      new B.Vector3(nx * 2 + 0.3, 0.8, nz * 2 + 0.3)
    );
    ps.color1 = new B.Color4(1, 1, 1, 0.9);
    ps.color2 = new B.Color4(c.r, c.g, c.b, 0.8);
    ps.colorDead = new B.Color4(c.r * 0.3, c.g * 0.3, c.b * 0.3, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.08; ps.maxLifeTime = 0.2;
    ps.emitRate = 200;
    ps.minEmitPower = 30; ps.maxEmitPower = 60;
    ps.gravity = new B.Vector3(0, -20, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.06;
    ps.disposeOnStop = true;
    ps.start();
  }

  spawnDamageNumber(x, z, dmg) {
    if (this._dmgCount >= MAX_DMG_NUMBERS) return;
    this._dmgCount++;

    const B = window.BABYLON;
    const plane = B.MeshBuilder.CreatePlane(`dmg`, { width: 24, height: 12 }, this.scene);
    plane.position.set(x + (Math.random() - 0.5) * 10, 25, z);
    plane.billboardMode = B.Mesh.BILLBOARDMODE_ALL;

    const tex = new B.DynamicTexture(`dtex-dmg`, { width: 128, height: 64 }, this.scene, false);
    const ctx = tex.getContext();
    ctx.font = 'bold 36px monospace';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillStyle = dmg > 0 ? '#ff4444' : '#44ff44';
    ctx.fillText(`${Math.abs(Math.round(dmg))}`, 64, 32);
    tex.update(); tex.hasAlpha = true;

    const mat = new B.StandardMaterial(`dmat-dmg`, this.scene);
    mat.diffuseTexture = tex; mat.emissiveTexture = tex;
    mat.disableLighting = true; mat.backFaceCulling = false;
    mat.useAlphaFromDiffuseTexture = true; mat.hasAlpha = true;
    plane.material = mat;

    const startY = 25;

    const posAnim = new B.Animation('dmgPosY', 'position.y', 100,
      B.Animation.ANIMATIONTYPE_FLOAT, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    posAnim.setKeys([
      { frame: 0, value: startY },
      { frame: 50, value: startY + 10 }
    ]);

    const alphaAnim = new B.Animation('dmgAlpha', 'alpha', 100,
      B.Animation.ANIMATIONTYPE_FLOAT, B.Animation.ANIMATIONLOOPMODE_CONSTANT);
    alphaAnim.setKeys([
      { frame: 0, value: 1 },
      { frame: 50, value: 0 }
    ]);

    const animatable = this.scene.beginDirectAnimation(plane, [posAnim], 0, 50, false);
    this.scene.beginDirectAnimation(mat, [alphaAnim], 0, 50, false);
    animatable.onAnimationEnd = () => {
      plane.dispose(); mat.dispose(); tex.dispose();
      this._dmgCount--;
    };
  }

  /** @private */
  _deathBurst(x, z, hexColor) {
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const ps = new B.ParticleSystem(`death-${++_psCounter}`, 20, this.scene);
    ps.emitter = new B.Vector3(x, 10, z);
    ps.createPointEmitter(new B.Vector3(-1, 1, -1), new B.Vector3(1, 1, 1));
    ps.color1 = new B.Color4(c.r, c.g, c.b, 1);
    ps.color2 = new B.Color4(1, 0.9, 0.7, 1);
    ps.colorDead = new B.Color4(c.r * 0.2, c.g * 0.2, c.b * 0.2, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.2; ps.maxLifeTime = 0.6;
    ps.emitRate = 200;
    ps.minEmitPower = 25; ps.maxEmitPower = 60;
    ps.gravity = new B.Vector3(0, -40, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.1;
    ps.disposeOnStop = true;
    ps.start();
  }

  /** @private */
  _cleanup(now) {
    this.active = this.active.filter(e => {
      if (now - e.created > 2000) { e.dispose(); return false; }
      return true;
    });
  }
}
