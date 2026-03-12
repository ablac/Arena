'use strict';

/**
 * Combat effects — death bursts, hit sparks, damage numbers, dodge ghosts.
 * @module renderer/effects
 */

import { parseColor } from './utils.js';

export class EffectRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Set<string>} */
    this.prevAlive = new Set();
    /** @type {Array<{dispose: Function, created: number}>} */
    this.active = [];
  }

  /**
   * Detect deaths and spawn effects.
   * @param {Array} bots
   */
  update(bots) {
    const now = Date.now();
    const alive = new Set();

    for (const bot of bots) {
      if (bot.is_alive) {
        alive.add(bot.bot_id);
      } else if (this.prevAlive.has(bot.bot_id)) {
        this._deathBurst(bot.position[0], bot.position[1], bot.avatar_color, now);
        this._flashDisc(bot.position[0], bot.position[1], now);
      }
    }

    this.prevAlive = alive;
    this._cleanup(now);
  }

  /**
   * Spawn hit sparks at a position.
   * @param {number} x @param {number} z @param {string} hexColor
   */
  spawnHitSparks(x, z, hexColor) {
    const B = window.BABYLON;
    const ps = new B.ParticleSystem(`sparks-${Date.now()}`, 30, this.scene);
    ps.emitter = new B.Vector3(x, 12, z);
    ps.createPointEmitter(new B.Vector3(-1, 1, -1), new B.Vector3(1, 1, 1));
    const c = parseColor(hexColor);
    ps.color1 = new B.Color4(1, 0.8, 0.3, 1);
    ps.color2 = new B.Color4(c.r, c.g, c.b, 1);
    ps.colorDead = new B.Color4(0.3, 0.1, 0, 0);
    ps.minSize = 1; ps.maxSize = 3;
    ps.minLifeTime = 0.1; ps.maxLifeTime = 0.4;
    ps.emitRate = 200;
    ps.minEmitPower = 15; ps.maxEmitPower = 40;
    ps.gravity = new B.Vector3(0, -50, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.1;
    ps.disposeOnStop = true;
    ps.start();
    this.active.push({ dispose: () => { if (!ps.isDisposed) ps.dispose(); }, created: Date.now() });
  }

  /**
   * Spawn floating damage number.
   * @param {number} x @param {number} z @param {number} dmg
   */
  spawnDamageNumber(x, z, dmg) {
    const B = window.BABYLON;
    const plane = B.MeshBuilder.CreatePlane(`dmg-${Date.now()}`, { width: 24, height: 12 }, this.scene);
    plane.rotation.x = Math.PI / 2;
    plane.position.set(x + (Math.random() - 0.5) * 10, 25, z);
    plane.billboardMode = B.Mesh.BILLBOARDMODE_ALL;

    const tex = new B.DynamicTexture(`dtex-dmg-${Date.now()}`, { width: 128, height: 64 }, this.scene, false);
    const ctx = tex.getContext();
    ctx.font = 'bold 36px monospace';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillStyle = dmg > 0 ? '#ff4444' : '#44ff44';
    ctx.fillText(`${Math.abs(Math.round(dmg))}`, 64, 32);
    tex.update(); tex.hasAlpha = true;

    const mat = new B.StandardMaterial(`dmat-${Date.now()}`, this.scene);
    mat.diffuseTexture = tex; mat.emissiveTexture = tex;
    mat.disableLighting = true; mat.backFaceCulling = false;
    mat.useAlphaFromDiffuseTexture = true; mat.hasAlpha = true;
    plane.material = mat;

    const created = Date.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const age = (Date.now() - created) / 1000;
      plane.position.y = 25 + age * 20;
      mat.alpha = Math.max(0, 1 - age * 1.5);
      if (age > 0.8) {
        this.scene.onBeforeRenderObservable.remove(obs);
        plane.dispose(); mat.dispose(); tex.dispose();
      }
    });
    this.active.push({ dispose: () => { plane.dispose(); mat.dispose(); tex.dispose(); }, created });
  }

  /** @private */
  _deathBurst(x, z, hexColor, now) {
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const ps = new B.ParticleSystem(`death-${now}`, 100, this.scene);
    ps.emitter = new B.Vector3(x, 10, z);
    ps.createPointEmitter(new B.Vector3(-1, 1, -1), new B.Vector3(1, 1, 1));
    ps.color1 = new B.Color4(c.r, c.g, c.b, 1);
    ps.color2 = new B.Color4(1, 0.9, 0.7, 1);
    ps.colorDead = new B.Color4(c.r * 0.2, c.g * 0.2, c.b * 0.2, 0);
    ps.minSize = 2; ps.maxSize = 6;
    ps.minLifeTime = 0.3; ps.maxLifeTime = 1.0;
    ps.emitRate = 400;
    ps.minEmitPower = 30; ps.maxEmitPower = 80;
    ps.gravity = new B.Vector3(0, -40, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.15;
    ps.disposeOnStop = true;
    ps.start();
    this.active.push({ dispose: () => { if (!ps.isDisposed) ps.dispose(); }, created: now });
  }

  /** @private Expanding white flash disc. */
  _flashDisc(x, z, now) {
    const B = window.BABYLON;
    const mesh = B.MeshBuilder.CreateDisc(`flash-${now}`, { radius: 8, tessellation: 20 }, this.scene);
    mesh.rotation.x = Math.PI / 2;
    mesh.position.set(x, 3, z);
    const mat = new B.StandardMaterial(`fmat-${now}`, this.scene);
    mat.emissiveColor = new B.Color3(1, 1, 1);
    mat.disableLighting = true; mat.alpha = 0.9; mat.backFaceCulling = false;
    mesh.material = mat;

    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const age = Date.now() - now;
      if (age > 250) {
        this.scene.onBeforeRenderObservable.remove(obs);
        mesh.dispose(); mat.dispose();
        return;
      }
      const t = age / 250;
      mat.alpha = 0.9 * (1 - t);
      mesh.scaling.setAll(1 + t * 8);
    });
    this.active.push({ dispose: () => { mesh.dispose(); mat.dispose(); }, created: now });
  }

  /** @private Remove old effects. */
  _cleanup(now) {
    this.active = this.active.filter(e => {
      if (now - e.created > 3000) { e.dispose(); return false; }
      return true;
    });
  }
}
