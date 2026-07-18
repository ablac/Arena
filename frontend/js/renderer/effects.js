'use strict';

/**
 * Combat effects — death bursts, hit sparks, damage numbers, dodge ghosts.
 * Optimized: reduced particle counts, pooled damage textures.
 * @module renderer/effects
 */

import { parseColor } from './utils.js';
import { isEnabled } from '../settings.js';

/** Max concurrent damage numbers to prevent buildup. */
const MAX_DMG_NUMBERS = 12;

/**
 * Capacity of every pooled transient particle system — the max any combat
 * burst in this file requests (staff explosion, 38). Pooling one shared size
 * lets a hit spark reuse the system a death burst just released instead of
 * paying the 5-GPU-buffer construction cost per event.
 */
const POOLED_PS_CAPACITY = 38;

/**
 * Tube thickness of the pooled unit ring (torus of diameter 1). Pooled rings
 * vary only by a uniform scale, so thickness tracks diameter at this fixed
 * ratio; the transient combat rings it replaces span ratios of 0.075-0.1,
 * which this mid-range value approximates fine at spectator distance.
 */
const RING_UNIT_THICKNESS = 0.085;

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
    /** @type {Map<string, number>} bot_id -> last-seen hp, for damage numbers */
    this.prevHp = new Map();
    /** @type {Array<{dispose: Function, created: number}>} */
    this.active = [];
    this._dmgCount = 0;
    this._glowTex = null;
    // Per-instance (NOT module-level) free list of transient particle
    // systems: the scene is disposed and rebuilt between rounds, so pooled
    // systems must die with their EffectRenderer.
    this._psPool = [];
    this._initGlowTexture();
  }

  /** @private Shared glow particle texture (same pattern as gameplay.js). */
  _initGlowTexture() {
    const B = window.BABYLON;
    this._glowTex = new B.DynamicTexture('fxGlowTex', 32, this.scene, false);
    const ctx = this._glowTex.getContext();
    const g = ctx.createRadialGradient(16, 16, 0, 16, 16, 16);
    g.addColorStop(0, 'rgba(255,255,255,1)');
    g.addColorStop(0.5, 'rgba(255,255,255,0.4)');
    g.addColorStop(1, 'rgba(255,255,255,0)');
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, 32, 32);
    this._glowTex.update();
    this._glowTex.hasAlpha = true;
  }

  /**
   * @private Acquire a pooled transient particle system. Pop-or-create; every
   * spawn site must fully reassign emitter/directions/colors/sizes/lifetimes/
   * emitRate/power/gravity/targetStopDuration AND blendMode (bow-miss dust is
   * BLENDMODE_STANDARD while everything else is ADD — state would bleed
   * otherwise). The system keeps one persistent PointParticleEmitter and one
   * emitter Vector3, so spawns copy values in place instead of allocating a
   * new emitter + vectors per event.
   */
  _acquirePS() {
    const B = window.BABYLON;
    let ps = this._psPool.pop();
    if (!ps) {
      ps = new B.ParticleSystem(`fx-ps-${++_psCounter}`, POOLED_PS_CAPACITY, this.scene);
      ps.createPointEmitter(new B.Vector3(0, 0, 0), new B.Vector3(0, 0, 0));
      ps.emitter = new B.Vector3(0, 0, 0);
    }
    return ps;
  }

  /**
   * @private Start a pooled transient particle system and hand its lifetime
   * to the wall-clock registry swept by update().
   *
   * Two Babylon traps live here. (1) A texture-less ParticleSystem never
   * animates (isReady() stays false), so targetStopDuration never fires and
   * the system stays in scene.particleSystems forever — the shared glow
   * texture is load-bearing, not cosmetic. (2) disposeOnStop calls dispose()
   * with disposeTexture=true, which would destroy that shared texture for
   * every other system; the registry is the only safe owner, and it now
   * reset()s the finished system back into the per-renderer pool instead of
   * disposing its GPU buffers per event (dispose(false) happens once, in
   * dispose()). The registry sweep runs from the WS-driven update() path, so
   * cleanup keeps working while a hidden/occluded tab has no rAF frames.
   */
  _launch(ps) {
    ps.particleTexture = this._glowTex;
    ps.start();
    this.active.push({
      created: Date.now(),
      dispose: () => {
        try {
          ps.stop();
          ps.reset();
          this._psPool.push(ps);
        } catch { /* scene torn down mid-flight */ }
      },
    });
  }

  /**
   * @private Acquire a pooled unit ring (torus, diameter 1) with its own
   * reusable StandardMaterial — the transient combat rings differ only by
   * diameter/thickness, so callers just scale by the target diameter.
   * Mirrors the _dmgPool pattern: pop-or-create, fully re-initialize on
   * acquire, hand back through _releaseRing() instead of dispose().
   */
  _acquireRing() {
    const B = window.BABYLON;
    if (!this._ringPool) this._ringPool = [];
    let entry = this._ringPool.pop();
    if (!entry || entry.mesh.isDisposed()) {
      const mesh = B.MeshBuilder.CreateTorus(`fx-ring-${++_psCounter}`, {
        diameter: 1,
        thickness: RING_UNIT_THICKNESS,
        tessellation: 32,
      }, this.scene);
      const mat = new B.StandardMaterial(`fx-ring-mat-${_psCounter}`, this.scene);
      mat.diffuseColor = B.Color3.Black();
      mat.disableLighting = true;
      mesh.material = mat;
      entry = { mesh, mat };
    }
    entry.mesh.parent = null;
    entry.mesh.position.set(0, 0, 0);
    entry.mesh.rotation.set(Math.PI / 2, 0, 0);
    entry.mesh.scaling.set(1, 1, 1);
    entry.mat.alpha = 1;
    entry.mesh.setEnabled(true);
    return entry;
  }

  /** @private Return a pooled ring for reuse instead of disposing it. */
  _releaseRing(entry) {
    entry.mesh.setEnabled(false);
    this._ringPool.push(entry);
  }

  /** @private Same pooling as _acquireRing, for a unit disc (radius 1). */
  _acquireDisc() {
    const B = window.BABYLON;
    if (!this._discPool) this._discPool = [];
    let entry = this._discPool.pop();
    if (!entry || entry.mesh.isDisposed()) {
      const mesh = B.MeshBuilder.CreateDisc(`fx-disc-${++_psCounter}`, {
        radius: 1,
        tessellation: 36,
      }, this.scene);
      const mat = new B.StandardMaterial(`fx-disc-mat-${_psCounter}`, this.scene);
      mat.diffuseColor = B.Color3.Black();
      mat.disableLighting = true;
      mesh.material = mat;
      entry = { mesh, mat };
    }
    entry.mesh.parent = null;
    entry.mesh.position.set(0, 0, 0);
    entry.mesh.rotation.set(Math.PI / 2, 0, 0);
    entry.mesh.scaling.set(1, 1, 1);
    entry.mat.alpha = 1;
    entry.mesh.setEnabled(true);
    return entry;
  }

  /** @private Return a pooled disc for reuse instead of disposing it. */
  _releaseDisc(entry) {
    entry.mesh.setEnabled(false);
    this._discPool.push(entry);
  }

  update(bots) {
    const now = Date.now();
    const alive = new Set();
    // Hidden/occluded tabs still receive WS states at 10Hz but render no
    // frames — skip spawning eye-candy there (state tracking continues).
    const spawnOk = !document.hidden;

    for (const bot of bots) {
      if (bot.is_alive) {
        alive.add(bot.bot_id);
        // Damage numbers (task-6, phase A): on an hp decrease for a bot that
        // was already alive last frame, pop a floating number at the victim.
        // spawnDamageNumber is a pre-built pooled system that had no caller.
        const prev = this.prevHp.get(bot.bot_id);
        if (prev != null && spawnOk && this.prevAlive.has(bot.bot_id) && bot.hp < prev) {
          this.spawnDamageNumber(bot.position[0], bot.position[1], prev - bot.hp);
        }
        this.prevHp.set(bot.bot_id, bot.hp);
      } else {
        if (this.prevAlive.has(bot.bot_id) && spawnOk) {
          this._deathBurst(bot.position[0], bot.position[1], bot.avatar_color);
        }
        // Reset on death so a respawn back to full hp is not read as a hit.
        this.prevHp.delete(bot.bot_id);
      }
    }

    this.prevAlive = alive;
    // Prune prevHp to the alive set so a bot that leaves the list without a
    // preceding is_alive:false frame (mid-match disconnect, between-round drop)
    // does not linger for the whole session; subsumes the death-branch delete.
    for (const id of this.prevHp.keys()) {
      if (!alive.has(id)) this.prevHp.delete(id);
    }
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
    if (!isEnabled('weaponImpactVfx', 'hitSparks')) return;
    const cfg = HIT_EFFECTS[weapon] || HIT_EFFECTS.sword;
    const B = window.BABYLON;
    const ps = this._acquirePS();
    ps.emitter.set(x, 12, z);
    ps.direction1.set(cfg.d1[0], cfg.d1[1], cfg.d1[2]);
    ps.direction2.set(cfg.d2[0], cfg.d2[1], cfg.d2[2]);
    ps.color1.set(cfg.c1[0], cfg.c1[1], cfg.c1[2], 1);
    const c = parseColor(hexColor);
    ps.color2.set(c.r, c.g, c.b, 1);
    ps.colorDead.set(cfg.dead[0], cfg.dead[1], cfg.dead[2], 0);
    ps.minSize = cfg.minSz; ps.maxSize = cfg.maxSz;
    ps.minLifeTime = cfg.minLife; ps.maxLifeTime = cfg.maxLife;
    ps.emitRate = cfg.rate;
    ps.minEmitPower = cfg.minPow; ps.maxEmitPower = cfg.maxPow;
    ps.gravity.set(0, -50, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = cfg.stop;
    this._launch(ps);
  }

  /**
   * Bow impact: sharper strike with a quick tracer flash and dust ping.
   * @param {number} x
   * @param {number} z
   * @param {string} hexColor
   */
  spawnBowImpact(x, z, hexColor, didHit = true, intensity = 1) {
    if (!isEnabled('weaponImpactVfx', 'bowImpact')) return;
    this.spawnHitSparks(x, z, hexColor, 'bow');
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const power = Math.max(1, intensity || 1);
    const ringSize = (didHit ? 10 : 8) * Math.min(1.35, 1 + (power - 1) * 0.18);
    const ringEntry = this._acquireRing();
    const ring = ringEntry.mesh;
    const ringMat = ringEntry.mat;
    ring.position.set(x, didHit ? 10.4 : 2.6, z);
    ring.scaling.set(ringSize, ringSize, ringSize);
    ringMat.emissiveColor.set(
      Math.min(1, c.r + (didHit ? 0.15 : 0.05)),
      Math.min(1, c.g + (didHit ? 0.15 : 0.05)),
      Math.min(1, c.b + (didHit ? 0.15 : 0.05)),
    );

    if (!didHit) {
      const dust = this._acquirePS();
      dust.emitter.set(x, 1.4, z);
      dust.direction1.set(-1.2, 0.3, -1.2);
      dust.direction2.set(1.2, 1.5, 1.2);
      dust.color1.set(0.6, 0.65, 0.72, 0.45);
      dust.color2.set(0.35, 0.4, 0.48, 0.22);
      dust.colorDead.set(0.15, 0.18, 0.2, 0);
      dust.minSize = 0.8; dust.maxSize = 2.4;
      dust.minLifeTime = 0.08; dust.maxLifeTime = 0.16;
      dust.emitRate = 120;
      dust.minEmitPower = 4; dust.maxEmitPower = 10;
      dust.gravity.set(0, -15, 0);
      dust.blendMode = B.ParticleSystem.BLENDMODE_STANDARD;
      dust.targetStopDuration = 0.06;
      this._launch(dust);
    }

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 180;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        this._releaseRing(ringEntry);
        return;
      }
      const s = ringSize * (1 + t * 1.4);
      ring.scaling.set(s, s, ringSize);
      ringMat.alpha = 1 - t;
    });
  }

  /**
   * Spawn dodge afterimage shimmer effect.
   * @param {number} x
   * @param {number} z
   * @param {string} hexColor
   */
  spawnDodgeEffect(x, z, hexColor) {
    if (!isEnabled('weaponImpactVfx', 'dodgeAfterimage')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const ps = this._acquirePS();
    ps.emitter.set(x, 10, z);
    ps.direction1.set(-1, 0.5, -1);
    ps.direction2.set(1, 0.5, 1);
    ps.color1.set(c.r, c.g, c.b, 0.8);
    ps.color2.set(1, 1, 1, 0.6);
    ps.colorDead.set(c.r, c.g, c.b, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.1; ps.maxLifeTime = 0.3;
    ps.emitRate = 200;
    ps.minEmitPower = 10; ps.maxEmitPower = 25;
    ps.gravity.set(0, -10, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.05;
    this._launch(ps);
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
    if (!isEnabled('weaponImpactVfx', 'shoveShockwave')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    // Direction from attacker to target
    const dx = tx - ax;
    const dz = tz - az;
    const len = Math.sqrt(dx * dx + dz * dz) || 1;
    const nx = dx / len;
    const nz = dz / len;

    const ps = this._acquirePS();
    ps.emitter.set(tx, 10, tz);
    // Blast outward in the push direction
    ps.direction1.set(nx * 0.5 - 0.3, 0.3, nz * 0.5 - 0.3);
    ps.direction2.set(nx * 2 + 0.3, 0.8, nz * 2 + 0.3);
    ps.color1.set(1, 1, 1, 0.9);
    ps.color2.set(c.r, c.g, c.b, 0.8);
    ps.colorDead.set(c.r * 0.3, c.g * 0.3, c.b * 0.3, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.08; ps.maxLifeTime = 0.2;
    ps.emitRate = 200;
    ps.minEmitPower = 30; ps.maxEmitPower = 60;
    ps.gravity.set(0, -20, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.06;
    this._launch(ps);
  }

  /**
   * Lightweight attack accent for melee/control weapons.
   * Adds a short-lived slash/thrust/bash read without overriding hit sparks.
   * @param {number} ax
   * @param {number} az
   * @param {number} tx
   * @param {number} tz
   * @param {string} hexColor
   * @param {string} [weapon='sword']
   */
  spawnWeaponStrike(ax, az, tx, tz, hexColor, weapon = 'sword') {
    if (!isEnabled('weaponImpactVfx', 'weaponStrike')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const dx = tx - ax;
    const dz = tz - az;
    const len = Math.max(1, Math.sqrt(dx * dx + dz * dz));
    const nx = dx / len;
    const nz = dz / len;
    const mx = ax + dx * 0.5;
    const mz = az + dz * 0.5;
    const rotY = Math.atan2(dx, dz);

    const strikeCfg = {
      sword:   { width: 3.1, height: 22, alpha: 0.46, lift: 11.5, scale: 1.42, hue: [1.0, 0.82, 0.38], trail: true, arc: 0.28 },
      daggers: { width: 1.5, height: 12, alpha: 0.34, lift: 10, scale: 1.15, hue: [1.0, 0.58, 0.22] },
      spear:   { width: 1.2, height: 24, alpha: 0.32, lift: 10, scale: 1.45, hue: [1.0, 0.55, 0.28] },
      shield:  { width: 5.8, height: 5.8, alpha: 0.28, lift: 9, scale: 1.18, hue: [0.82, 0.92, 1.0] },
      grapple: { width: 1.4, height: 16, alpha: 0.34, lift: 10, scale: 1.4, hue: [0.5, 0.95, 1.0] },
    }[weapon] || { width: 2.2, height: 14, alpha: 0.34, lift: 10, scale: 1.2, hue: [1.0, 0.82, 0.38] };

    const slash = B.MeshBuilder.CreatePlane(`strike-${++_psCounter}`, {
      width: strikeCfg.width,
      height: strikeCfg.height,
      sideOrientation: B.Mesh.DOUBLESIDE,
    }, this.scene);
    slash.position.set(mx + nx * 3.5, strikeCfg.lift, mz + nz * 3.5);
    slash.rotation.y = rotY;
    slash.rotation.x = weapon === 'shield' ? Math.PI / 2 : Math.PI / 2.4;
    slash.rotation.z = weapon === 'shield' ? 0 : Math.PI / 2;
    const slashMat = new B.StandardMaterial(`strike-mat-${_psCounter}`, this.scene);
    slashMat.diffuseColor = B.Color3.Black();
    slashMat.emissiveColor = new B.Color3(
      Math.min(1, strikeCfg.hue[0] * 0.65 + c.r * 0.55),
      Math.min(1, strikeCfg.hue[1] * 0.65 + c.g * 0.55),
      Math.min(1, strikeCfg.hue[2] * 0.65 + c.b * 0.55),
    );
    slashMat.alpha = strikeCfg.alpha;
    slashMat.disableLighting = true;
    slash.material = slashMat;

    let wake = null;
    let wakeMat = null;
    if (strikeCfg.trail) {
      wake = B.MeshBuilder.CreateDisc(`strike-wake-${++_psCounter}`, {
        radius: 5.8,
        tessellation: 26,
        arc: 0.68,
      }, this.scene);
      wake.position.copyFrom(slash.position);
      wake.rotation.x = Math.PI / 2;
      wake.rotation.y = rotY - strikeCfg.arc;
      wake.scaling.set(1.05, 1.05, 1);
      wakeMat = new B.StandardMaterial(`strike-wake-mat-${_psCounter}`, this.scene);
      wakeMat.diffuseColor = B.Color3.Black();
      wakeMat.emissiveColor = new B.Color3(
        Math.min(1, c.r * 0.62 + 0.42),
        Math.min(1, c.g * 0.58 + 0.28),
        Math.min(1, c.b * 0.38 + 0.12),
      );
      wakeMat.disableLighting = true;
      wakeMat.alpha = 0.24;
      wake.material = wakeMat;
    }

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / (weapon === 'daggers' ? 110 : 150);
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        slash.dispose();
        slashMat.dispose();
        if (wake) wake.dispose();
        if (wakeMat) wakeMat.dispose();
        return;
      }
      const pulse = 1 + Math.sin(t * Math.PI) * (strikeCfg.scale - 1);
      slash.scaling.set(pulse, pulse, pulse);
      slash.position.x = mx + nx * (3.5 + t * 3.2);
      slash.position.z = mz + nz * (3.5 + t * 3.2);
      slash.position.y = strikeCfg.lift + Math.sin(t * Math.PI) * 1.4;
      slashMat.alpha = Math.max(0, strikeCfg.alpha * (1 - t));
      if (wake && wakeMat) {
        wake.position.copyFrom(slash.position);
        wake.position.y = 1.25 + Math.sin(t * Math.PI) * 0.35;
        wake.rotation.y = rotY - strikeCfg.arc + t * 0.55;
        const sweep = 1 + t * 0.65;
        wake.scaling.set(sweep, sweep, 1);
        wakeMat.alpha = Math.max(0, 0.24 * (1 - t));
      }
    });
  }

  spawnShieldBash(ax, az, tx, tz, hexColor = '#bfe3ff') {
    if (!isEnabled('weaponImpactVfx', 'shieldBash')) return;
    const c = parseColor(hexColor);
    this.spawnWeaponStrike(ax, az, tx, tz, hexColor, 'shield');
    this.spawnHitSparks(tx, tz, hexColor, 'shield');

    const ringEntry = this._acquireRing();
    const ring = ringEntry.mesh;
    const ringMat = ringEntry.mat;
    ring.position.set(tx, 2.2, tz);
    ring.scaling.set(18, 18, 18);
    ringMat.emissiveColor.set(Math.min(1, c.r + 0.2), Math.min(1, c.g + 0.2), Math.min(1, c.b + 0.2));

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 180;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        this._releaseRing(ringEntry);
        return;
      }
      const s = 18 * (1 + t * 1.2);
      ring.scaling.set(s, s, 18);
      ringMat.alpha = 1 - t;
    });
  }

  spawnSpearBrace(ax, az, tx, tz, hexColor = '#ffe38a') {
    if (!isEnabled('weaponImpactVfx', 'spearBrace')) return;
    const c = parseColor(hexColor);
    this.spawnWeaponStrike(ax, az, tx, tz, hexColor, 'spear');
    this.spawnHitSparks(tx, tz, hexColor, 'spear');

    const ringEntry = this._acquireRing();
    const ring = ringEntry.mesh;
    const mat = ringEntry.mat;
    ring.position.set(tx, 10.8, tz);
    ring.scaling.set(12, 12, 12);
    mat.emissiveColor.set(Math.min(1, c.r + 0.15), Math.min(1, c.g + 0.1), Math.min(1, c.b + 0.05));

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 220;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        this._releaseRing(ringEntry);
        return;
      }
      const s = 12 * (1 + t * 1.1);
      ring.scaling.set(s, s, 12);
      mat.alpha = 1 - t;
    });
  }

  spawnBackstab(ax, az, tx, tz, hexColor = '#ff8f47') {
    if (!isEnabled('weaponImpactVfx', 'backstab')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    this.spawnWeaponStrike(ax, az, tx, tz, hexColor, 'daggers');
    this.spawnHitSparks(tx, tz, hexColor, 'daggers');

    const mark = B.MeshBuilder.CreatePlane(`backstab-mark-${++_psCounter}`, {
      width: 8,
      height: 16,
      sideOrientation: B.Mesh.DOUBLESIDE,
    }, this.scene);
    mark.position.set(tx, 12, tz);
    mark.rotation.x = Math.PI / 2.6;
    mark.rotation.y = Math.atan2(tx - ax, tz - az);
    const markMat = new B.StandardMaterial(`backstab-mark-mat-${_psCounter}`, this.scene);
    markMat.diffuseColor = B.Color3.Black();
    markMat.emissiveColor = new B.Color3(Math.min(1, c.r + 0.12), Math.min(1, c.g * 0.5 + 0.18), Math.min(1, c.b * 0.35 + 0.08));
    markMat.disableLighting = true;
    markMat.alpha = 0.58;
    mark.material = markMat;

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 140;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        mark.dispose();
        markMat.dispose();
        return;
      }
      mark.scaling.set(1 + t * 0.45, 1 + t * 0.45, 1);
      mark.position.y = 12 + Math.sin(t * Math.PI) * 1.8;
      markMat.alpha = Math.max(0, 0.58 * (1 - t));
    });
  }

  spawnGrappleSlam(ax, az, tx, tz, hexColor = '#59f1ff') {
    if (!isEnabled('weaponImpactVfx', 'grappleSlam')) return;
    const c = parseColor(hexColor);
    this.spawnHitSparks(tx, tz, hexColor, 'grapple');
    const burstEntry = this._acquireRing();
    const burst = burstEntry.mesh;
    const mat = burstEntry.mat;
    burst.position.set(tx, 10.6, tz);
    burst.scaling.set(14, 14, 14);
    mat.emissiveColor.set(Math.min(1, c.r + 0.2), Math.min(1, c.g + 0.18), Math.min(1, c.b + 0.22));

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 260;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        this._releaseRing(burstEntry);
        return;
      }
      const s = 14 * (1 + t * 1.6);
      burst.scaling.set(s, s, 14);
      mat.alpha = 1 - t;
    });
  }

  spawnDamageNumber(x, z, dmg) {
    if (!isEnabled('hitReactions', 'floatingDamageNumbers')) return;
    if (this._dmgCount >= MAX_DMG_NUMBERS) return;
    this._dmgCount++;

    const B = window.BABYLON;

    // Pooled: plane + DynamicTexture + material are reused across numbers.
    // Allocating a 128x64 canvas and uploading a fresh GPU texture on every
    // hit was steady churn in fights; now we just redraw the pooled canvas.
    if (!this._dmgPool) this._dmgPool = [];
    let entry = this._dmgPool.pop();
    if (!entry || entry.plane.isDisposed()) {
      const plane = B.MeshBuilder.CreatePlane(`dmg`, { width: 24, height: 12 }, this.scene);
      plane.billboardMode = B.Mesh.BILLBOARDMODE_ALL;
      const tex = new B.DynamicTexture(`dtex-dmg`, { width: 128, height: 64 }, this.scene, false);
      tex.hasAlpha = true;
      const mat = new B.StandardMaterial(`dmat-dmg`, this.scene);
      mat.diffuseTexture = tex; mat.emissiveTexture = tex;
      mat.disableLighting = true; mat.backFaceCulling = false;
      mat.useAlphaFromDiffuseTexture = true; mat.hasAlpha = true;
      plane.material = mat;
      entry = { plane, tex, mat };
    }

    const { plane, tex, mat } = entry;
    plane.position.set(x + (Math.random() - 0.5) * 10, 25, z);
    plane.setEnabled(true);
    mat.alpha = 1;

    const ctx = tex.getContext();
    ctx.clearRect(0, 0, 128, 64);
    // Magnitude styling: a big hit reads bigger and hotter than a chip of
    // damage, so the eye tracks the decisive blows at spectator distance.
    const mag = Math.abs(dmg);
    const fontPx = Math.round(Math.min(52, 30 + mag * 0.7));
    ctx.font = `bold ${fontPx}px monospace`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillStyle = dmg <= 0 ? '#44ff44' : (mag >= 25 ? '#ffdd33' : '#ff4444');
    ctx.fillText(`${Math.abs(Math.round(dmg))}`, 64, 32);
    tex.update();
    // A big hit gets a brief scale punch on the pooled plane.
    plane.scaling.setAll(mag >= 25 ? 1.35 : 1);

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
      plane.setEnabled(false);
      this._dmgPool.push(entry);
      this._dmgCount--;
    };
  }

  /** @private */
  _deathBurst(x, z, hexColor) {
    if (!isEnabled('deathEffects', 'deathBurst')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const ps = this._acquirePS();
    ps.emitter.set(x, 10, z);
    ps.direction1.set(-1, 1, -1);
    ps.direction2.set(1, 1, 1);
    ps.color1.set(c.r, c.g, c.b, 1);
    ps.color2.set(1, 0.9, 0.7, 1);
    ps.colorDead.set(c.r * 0.2, c.g * 0.2, c.b * 0.2, 0);
    ps.minSize = 2; ps.maxSize = 5;
    ps.minLifeTime = 0.2; ps.maxLifeTime = 0.6;
    ps.emitRate = 200;
    ps.minEmitPower = 25; ps.maxEmitPower = 60;
    ps.gravity.set(0, -40, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.1;
    this._launch(ps);

    // A kill is the most spectator-tracked event; the burst alone was the
    // smallest effect in this file. Three additions, all self-disposing
    // patterns proven elsewhere in this file. Deaths are rare events, so the
    // extra ~2 meshes + 1 particle system per death are negligible.

    // (1) Expanding avatar-tinted shockwave ring (shield-bash ring pattern).
    const ringEntry = this._acquireRing();
    const ring = ringEntry.mesh;
    const ringMat = ringEntry.mat;
    ring.position.set(x, 2.2, z);
    ring.scaling.set(26, 26, 26);
    ringMat.emissiveColor.set(Math.min(1, c.r + 0.2), Math.min(1, c.g + 0.2), Math.min(1, c.b + 0.2));
    const ringStart = performance.now();
    const ringObs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - ringStart) / 320;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(ringObs);
        this._releaseRing(ringEntry);
        return;
      }
      const s = 26 * (1 + t * 1.6);
      ring.scaling.set(s, s, 26);
      ringMat.alpha = 1 - t;
    });

    // (2) Vertical light pillar (teleport-shell pattern): a brief beam that
    // marks the elimination spot even at overview zoom. Stays un-pooled: it
    // is a cylinder, so the unit ring/disc pools cannot approximate it by
    // scaling — and deaths are rare enough that this stays negligible.
    const pillar = B.MeshBuilder.CreateCylinder(`death-pillar-${++_psCounter}`, {
      height: 22,
      diameter: 6,
      tessellation: 12,
    }, this.scene);
    pillar.position.set(x, 11, z);
    const pillarMat = new B.StandardMaterial(`death-pillar-mat-${_psCounter}`, this.scene);
    pillarMat.diffuseColor = B.Color3.Black();
    pillarMat.emissiveColor = new B.Color3(Math.min(1, c.r + 0.2), Math.min(1, c.g + 0.2), Math.min(1, c.b + 0.2));
    pillarMat.disableLighting = true;
    pillarMat.alpha = 0.35;
    pillar.material = pillarMat;
    const pillarStart = performance.now();
    const pillarObs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - pillarStart) / 400;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(pillarObs);
        pillar.dispose();
        pillarMat.dispose();
        return;
      }
      const ys = 0.6 + t * 0.8;
      pillar.scaling.set(1, ys, 1);
      pillar.position.y = 11 * ys;
      pillarMat.alpha = 0.35 * (1 - t);
    });

    // (3) Slow lingering embers that hang in the air after the flash.
    const embers = this._acquirePS();
    embers.emitter.set(x, 12, z);
    embers.direction1.set(-0.6, 0.4, -0.6);
    embers.direction2.set(0.6, 1, 0.6);
    embers.color1.set(Math.min(1, c.r + 0.3), Math.min(1, c.g + 0.3), Math.min(1, c.b + 0.3), 0.9);
    embers.color2.set(1, 0.8, 0.5, 0.8);
    embers.colorDead.set(c.r * 0.3, c.g * 0.3, c.b * 0.3, 0);
    embers.minSize = 1; embers.maxSize = 2.5;
    embers.minLifeTime = 0.4; embers.maxLifeTime = 0.9;
    embers.emitRate = 90;
    embers.minEmitPower = 8; embers.maxEmitPower = 20;
    embers.gravity.set(0, -8, 0);
    embers.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    embers.targetStopDuration = 0.15;
    this._launch(embers);
  }

  /**
   * Spawn a grapple line + hook animation.
   * `mode=pull` means the chain retracts toward the shooter.
   * `mode=anchor` means the chain stays anchored while the user is pulled in.
   */
  spawnGrappleEffect(ax, az, tx, tz, opts = {}) {
    if (!isEnabled('weaponImpactVfx', 'grappleLine')) return;
    const B = window.BABYLON;
    const scene = this.scene;
    const color = parseColor(opts.color || '#59f1ff');
    const CHAIN_COLOR = new B.Color3(color.r, color.g, color.b);
    const CHAIN_Y = 12;
    const mode = opts.mode || 'pull';
    const endX = typeof opts.endX === 'number' ? opts.endX : tx;
    const endZ = typeof opts.endZ === 'number' ? opts.endZ : tz;
    const dx = tx - ax;
    const dz = tz - az;
    const travelYaw = Math.atan2(dx, dz);

    const chainMat = new B.StandardMaterial(`grapple-chain-mat-${++_psCounter}`, scene);
    chainMat.diffuseColor = CHAIN_COLOR.scale(0.15);
    chainMat.emissiveColor = CHAIN_COLOR.scale(1.05);
    chainMat.disableLighting = true;
    chainMat.alpha = 0.92;

    const root = new B.TransformNode(`grapple-root-${_psCounter}`, scene);
    const line = B.MeshBuilder.CreateCylinder(`gline-${_psCounter}`, {
      height: 1,
      diameter: 1.16,
      tessellation: 10,
    }, scene);
    line.material = chainMat;
    line.parent = root;

    const coreMat = new B.StandardMaterial(`grapple-core-mat-${++_psCounter}`, scene);
    coreMat.diffuseColor = B.Color3.Black();
    coreMat.emissiveColor = CHAIN_COLOR.scale(1.4);
    coreMat.disableLighting = true;
    coreMat.alpha = 0.78;
    const core = B.MeshBuilder.CreateCylinder(`gcore-${_psCounter}`, {
      height: 1,
      diameter: 0.36,
      tessellation: 8,
    }, scene);
    core.material = coreMat;
    core.parent = root;

    const hookMat = new B.StandardMaterial(`ghook-mat-${++_psCounter}`, scene);
    hookMat.diffuseColor = B.Color3.Black();
    hookMat.emissiveColor = new B.Color3(
      Math.min(1, color.r + 0.28),
      Math.min(1, color.g + 0.22),
      Math.min(1, color.b + 0.18),
    );
    hookMat.disableLighting = true;
    const hook = B.MeshBuilder.CreateCylinder(`ghook-head-${_psCounter}`, {
      height: 6.4, diameterTop: 0.35, diameterBottom: 2.6, tessellation: 6
    }, scene);
    hook.material = hookMat;
    hook.position.set(tx, CHAIN_Y, tz);
    hook.rotation.z = Math.PI / 2;
    hook.rotation.y = travelYaw;
    hook.parent = root;

    const flare = B.MeshBuilder.CreateTorus(`ghook-flare-${++_psCounter}`, {
      diameter: 4.6, thickness: 0.35, tessellation: 18,
    }, scene);
    flare.position.copyFrom(hook.position);
    flare.rotation.x = Math.PI / 2;
    flare.material = hookMat;
    flare.parent = root;

    // Spark particles at both ends
    const spawnSparks = (x, z) => {
      const ps = this._acquirePS();
      ps.emitter.set(x, CHAIN_Y, z);
      ps.direction1.set(-1, 1, -1);
      ps.direction2.set(1, 2, 1);
      ps.color1.set(color.r, color.g, color.b, 1);
      ps.color2.set(Math.min(1, color.r + 0.3), Math.min(1, color.g + 0.2), Math.min(1, color.b + 0.1), 1);
      ps.colorDead.set(0, 0.3, 0.5, 0);
      ps.minSize = 0.5; ps.maxSize = 2;
      ps.minLifeTime = 0.05; ps.maxLifeTime = 0.15;
      // 200/s x 0.4s previously outran the old capacity-15 system anyway;
      // 100/s keeps the same visible density inside the shared pooled
      // capacity without mid-burst starvation.
      ps.emitRate = 100;
      ps.minEmitPower = 15; ps.maxEmitPower = 40;
      ps.gravity.set(0, -30, 0);
      ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      ps.targetStopDuration = 0.4;
      this._launch(ps);
      return ps;
    };

    spawnSparks(ax, az);
    spawnSparks(tx, tz);

    const startTime = performance.now();
    const APPEAR_MS = 240;
    const PULL_MS = 240;
    const FADE_MS = 200;
    const TOTAL_MS = APPEAR_MS + PULL_MS + FADE_MS;

    const updateLine = (sx, sz, ex, ez, arc = 0) => {
      const midX = (sx + ex) * 0.5;
      const midZ = (sz + ez) * 0.5;
      const dx2 = ex - sx;
      const dz2 = ez - sz;
      const len = Math.max(0.001, Math.sqrt(dx2 * dx2 + dz2 * dz2));
      const yaw = Math.atan2(dx2, dz2);
      const pitch = Math.atan2(arc * 2, len);
      const midY = CHAIN_Y + arc * 0.5;

      line.position.set(midX, midY, midZ);
      line.rotation.set(Math.PI / 2 - pitch, yaw, 0);
      line.scaling.set(1, len, 1);

      core.position.copyFrom(line.position);
      core.rotation.copyFrom(line.rotation);
      core.scaling.set(1, len, 1);
    };

    const observer = scene.onBeforeRenderObservable.add(() => {
      const elapsed = performance.now() - startTime;

      if (elapsed < APPEAR_MS) {
        const progress = elapsed / APPEAR_MS;
        const hx = ax + dx * progress;
        const hz = az + dz * progress;
        updateLine(ax, az, hx, hz, 1.5 + (1 - progress) * 2.2);
        hook.position.set(hx, CHAIN_Y, hz);
        hook.rotation.y = travelYaw;
        flare.position.copyFrom(hook.position);
        flare.scaling.set(0.75 + progress * 0.45, 0.75 + progress * 0.45, 1);
      } else if (elapsed < APPEAR_MS + PULL_MS) {
        const pullT = (elapsed - APPEAR_MS) / PULL_MS;
        const hx = mode === 'anchor' ? tx : ax + dx * (1 - pullT * 0.82);
        const hz = mode === 'anchor' ? tz : az + dz * (1 - pullT * 0.82);
        const sx = mode === 'anchor' ? ax + dx * pullT * 0.35 : ax;
        const sz = mode === 'anchor' ? az + dz * pullT * 0.35 : az;
        updateLine(sx, sz, hx, hz, Math.sin(pullT * Math.PI) * 1.2);
        hook.position.set(hx, CHAIN_Y, hz);
        flare.position.copyFrom(hook.position);
        flare.scaling.set(1 + Math.sin(pullT * Math.PI) * 0.2, 1 + Math.sin(pullT * Math.PI) * 0.2, 1);
      } else if (elapsed < TOTAL_MS) {
        const fadeT = (elapsed - APPEAR_MS - PULL_MS) / FADE_MS;
        chainMat.alpha = 1 - fadeT;
        coreMat.alpha = 0.78 * (1 - fadeT);
        line.visibility = 1 - fadeT;
        core.visibility = 1 - fadeT;
        hook.visibility = 1 - fadeT;
        flare.visibility = 1 - fadeT;
      } else {
        scene.onBeforeRenderObservable.remove(observer);
        line.dispose();
        core.dispose();
        hook.dispose();
        flare.dispose();
        root.dispose();
        chainMat.dispose();
        coreMat.dispose();
        hookMat.dispose();
      }
    });

    // Subtle impact burst where the pull resolves.
    spawnSparks(endX, endZ);
  }

  /**
   * Spawn a mine detonation with a hot core, shock ring, and debris burst.
   * @param {number} x
   * @param {number} z
   * @param {number} [radius=20]
   */
  spawnMineExplosion(x, z, radius = 20) {
    if (!isEnabled('weaponImpactVfx', 'mineExplosion')) return;
    const B = window.BABYLON;
    const blastRadius = Math.max(12, radius);

    const ring = B.MeshBuilder.CreateTorus(`mine-ring-${++_psCounter}`, {
      diameter: blastRadius * 1.4,
      thickness: Math.max(1.2, blastRadius * 0.08),
      tessellation: 32,
    }, this.scene);
    const ringMat = new B.StandardMaterial(`mine-ring-mat-${_psCounter}`, this.scene);
    ringMat.emissiveColor = new B.Color3(1.0, 0.45, 0.1);
    ringMat.diffuseColor = B.Color3.Black();
    ringMat.disableLighting = true;
    ring.material = ringMat;
    ring.position.set(x, 2, z);
    ring.rotation.x = Math.PI / 2;

    const core = B.MeshBuilder.CreateSphere(`mine-core-${++_psCounter}`, {
      diameter: blastRadius * 0.35,
      segments: 10,
    }, this.scene);
    const coreMat = new B.StandardMaterial(`mine-core-mat-${_psCounter}`, this.scene);
    coreMat.emissiveColor = new B.Color3(1.0, 0.8, 0.2);
    coreMat.diffuseColor = B.Color3.Black();
    coreMat.disableLighting = true;
    core.material = coreMat;
    core.position.set(x, 4, z);

    const scorch = B.MeshBuilder.CreateDisc(`mine-scorch-${++_psCounter}`, {
      radius: blastRadius * 0.55,
      tessellation: 28,
    }, this.scene);
    scorch.rotation.x = Math.PI / 2;
    scorch.position.set(x, 0.25, z);
    const scorchMat = new B.StandardMaterial(`mine-scorch-mat-${_psCounter}`, this.scene);
    scorchMat.diffuseColor = B.Color3.Black();
    scorchMat.emissiveColor = new B.Color3(0.18, 0.08, 0.02);
    scorchMat.disableLighting = true;
    scorchMat.alpha = 0.42;
    scorch.material = scorchMat;

    const ps = this._acquirePS();
    ps.emitter.set(x, 2, z);
    ps.direction1.set(-3, 1, -3);
    ps.direction2.set(3, 6, 3);
    ps.color1.set(1, 0.85, 0.3, 1);
    ps.color2.set(1, 0.35, 0.05, 0.9);
    ps.colorDead.set(0.2, 0.05, 0.02, 0);
    ps.minSize = 1.5; ps.maxSize = 4.5;
    ps.minLifeTime = 0.12; ps.maxLifeTime = 0.28;
    ps.emitRate = 240;
    ps.minEmitPower = 18; ps.maxEmitPower = 45;
    ps.gravity.set(0, -35, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.10;
    this._launch(ps);

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 260;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        ring.dispose();
        ringMat.dispose();
        core.dispose();
        coreMat.dispose();
        scorch.dispose();
        scorchMat.dispose();
        return;
      }
      ring.scaling.set(1 + t * 1.6, 1 + t * 1.6, 1);
      ringMat.alpha = 1 - t;
      core.scaling.set(1 + t * 1.1, 1 + t * 1.1, 1 + t * 1.1);
      coreMat.alpha = 1 - t;
      scorchMat.alpha = Math.max(0, 0.42 - t * 0.12);
    });
  }

  /**
   * Spawn a staff detonation: arcane ring + flash + plume.
   * This is the actual AoE blast and should happen when the delayed staff
   * impact expires, whether or not any bot is hit.
   * @param {number} x
   * @param {number} z
   * @param {number} [radius=20]
   * @param {string} [hexColor='#8d4dff']
   */
  spawnStaffExplosion(x, z, radius = 20, hexColor = '#8d4dff') {
    if (!isEnabled('weaponImpactVfx', 'staffExplosion')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const blastRadius = Math.max(16, radius);

    const ring = B.MeshBuilder.CreateTorus(`staff-ring-${++_psCounter}`, {
      diameter: blastRadius * 1.6,
      thickness: Math.max(1.4, blastRadius * 0.08),
      tessellation: 40,
    }, this.scene);
    ring.position.set(x, 1.4, z);
    ring.rotation.x = Math.PI / 2;
    const ringMat = new B.StandardMaterial(`staff-ring-mat-${_psCounter}`, this.scene);
    ringMat.diffuseColor = B.Color3.Black();
    ringMat.emissiveColor = new B.Color3(
      Math.min(1, c.r + 0.2),
      Math.min(1, c.g + 0.08),
      Math.min(1, c.b + 0.28),
    );
    ringMat.disableLighting = true;
    ring.material = ringMat;

    const disc = B.MeshBuilder.CreateDisc(`staff-disc-${++_psCounter}`, {
      radius: blastRadius * 0.65,
      tessellation: 34,
    }, this.scene);
    disc.rotation.x = Math.PI / 2;
    disc.position.set(x, 0.25, z);
    const discMat = new B.StandardMaterial(`staff-disc-mat-${_psCounter}`, this.scene);
    discMat.diffuseColor = B.Color3.Black();
    discMat.emissiveColor = new B.Color3(c.r * 0.9, c.g * 0.55, c.b);
    discMat.disableLighting = true;
    discMat.alpha = 0.28;
    disc.material = discMat;

    const flash = B.MeshBuilder.CreateSphere(`staff-flash-${++_psCounter}`, {
      diameter: blastRadius * 0.4,
      segments: 10,
    }, this.scene);
    flash.position.set(x, 6, z);
    const flashMat = new B.StandardMaterial(`staff-flash-mat-${_psCounter}`, this.scene);
    flashMat.diffuseColor = B.Color3.Black();
    flashMat.emissiveColor = new B.Color3(1.0, 0.8, 1.0);
    flashMat.disableLighting = true;
    flash.material = flashMat;

    const ps = this._acquirePS();
    ps.emitter.set(x, 3, z);
    ps.direction1.set(-2.2, 0.8, -2.2);
    ps.direction2.set(2.2, 5.8, 2.2);
    ps.color1.set(Math.min(1, c.r + 0.25), Math.min(1, c.g + 0.1), Math.min(1, c.b + 0.2), 1);
    ps.color2.set(0.95, 0.85, 1.0, 0.75);
    ps.colorDead.set(c.r * 0.25, c.g * 0.15, c.b * 0.25, 0);
    ps.minSize = 1.4; ps.maxSize = 4.6;
    ps.minLifeTime = 0.14; ps.maxLifeTime = 0.26;
    ps.emitRate = 260;
    ps.minEmitPower = 14; ps.maxEmitPower = 34;
    ps.gravity.set(0, -18, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.08;
    this._launch(ps);

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 260;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        ring.dispose();
        ringMat.dispose();
        disc.dispose();
        discMat.dispose();
        flash.dispose();
        flashMat.dispose();
        return;
      }
      const ringScale = 1 + t * 1.5;
      ring.scaling.set(ringScale, ringScale, 1);
      disc.scaling.set(1 + t * 0.85, 1 + t * 0.85, 1);
      flash.scaling.set(1 + t * 1.2, 1 + t * 1.2, 1 + t * 1.2);
      ringMat.alpha = 1 - t;
      discMat.alpha = Math.max(0, 0.28 - t * 0.2);
      flashMat.alpha = Math.max(0, 0.95 - t * 1.3);
    });
  }

  spawnCapturePadPulse(x, z, radius = 36, hexColor = '#7ef7ff') {
    if (!isEnabled('objectiveIndicators', 'capturePadPulse')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const baseRadius = Math.max(18, radius);

    const ring = B.MeshBuilder.CreateTorus(`cap-ring-${++_psCounter}`, {
      diameter: baseRadius * 1.4,
      thickness: Math.max(1.4, baseRadius * 0.06),
      tessellation: 38,
    }, this.scene);
    ring.position.set(x, 1.6, z);
    ring.rotation.x = Math.PI / 2;
    const ringMat = new B.StandardMaterial(`cap-ring-mat-${_psCounter}`, this.scene);
    ringMat.diffuseColor = B.Color3.Black();
    ringMat.emissiveColor = new B.Color3(
      Math.min(1, c.r + 0.25),
      Math.min(1, c.g + 0.25),
      Math.min(1, c.b + 0.15),
    );
    ringMat.disableLighting = true;
    ring.material = ringMat;

    const disc = B.MeshBuilder.CreateDisc(`cap-disc-${++_psCounter}`, {
      radius: baseRadius * 0.55,
      tessellation: 36,
    }, this.scene);
    disc.rotation.x = Math.PI / 2;
    disc.position.set(x, 0.24, z);
    const discMat = new B.StandardMaterial(`cap-disc-mat-${_psCounter}`, this.scene);
    discMat.diffuseColor = B.Color3.Black();
    discMat.emissiveColor = new B.Color3(c.r * 0.95, c.g * 0.95, c.b * 0.85);
    discMat.disableLighting = true;
    discMat.alpha = 0.26;
    disc.material = discMat;

    const ps = this._acquirePS();
    ps.emitter.set(x, 2.2, z);
    ps.direction1.set(-2.5, 1.0, -2.5);
    ps.direction2.set(2.5, 5.5, 2.5);
    ps.color1.set(Math.min(1, c.r + 0.18), Math.min(1, c.g + 0.18), Math.min(1, c.b + 0.18), 1);
    ps.color2.set(1, 1, 1, 0.8);
    ps.colorDead.set(c.r * 0.25, c.g * 0.25, c.b * 0.25, 0);
    ps.minSize = 1.4; ps.maxSize = 4.2;
    ps.minLifeTime = 0.14; ps.maxLifeTime = 0.24;
    ps.emitRate = 220;
    ps.minEmitPower = 12; ps.maxEmitPower = 28;
    ps.gravity.set(0, -12, 0);
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    ps.targetStopDuration = 0.08;
    this._launch(ps);

    const start = performance.now();
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      const t = (performance.now() - start) / 320;
      if (t >= 1) {
        this.scene.onBeforeRenderObservable.remove(obs);
        ring.dispose();
        ringMat.dispose();
        disc.dispose();
        discMat.dispose();
        return;
      }
      ring.scaling.set(1 + t * 1.7, 1 + t * 1.7, 1);
      disc.scaling.set(1 + t * 0.8, 1 + t * 0.8, 1);
      ringMat.alpha = 1 - t;
      discMat.alpha = Math.max(0, 0.26 - t * 0.18);
    });
  }

  /**
   * Spawn matching bursts at teleport entry and exit.
   * @param {number} fromX
   * @param {number} fromZ
   * @param {number} toX
   * @param {number} toZ
   * @param {string} [hexColor='#00ffff']
   */
  spawnTeleportBurst(fromX, fromZ, toX, toZ, hexColor = '#00ffff') {
    if (!isEnabled('weaponImpactVfx', 'teleportBurst')) return;
    const B = window.BABYLON;
    const c = parseColor(hexColor);
    const burstAt = (x, z, invert = false) => {
      const ps = this._acquirePS();
      ps.emitter.set(x, 8, z);
      ps.direction1.set(-2, invert ? -1 : 1, -2);
      ps.direction2.set(2, invert ? 4 : 7, 2);
      ps.color1.set(c.r, c.g, c.b, 1);
      ps.color2.set(1, 1, 1, 0.9);
      ps.colorDead.set(c.r * 0.2, c.g * 0.2, c.b * 0.2, 0);
      ps.minSize = 1.5; ps.maxSize = 5;
      ps.minLifeTime = 0.08; ps.maxLifeTime = 0.22;
      ps.emitRate = 220;
      ps.minEmitPower = 12; ps.maxEmitPower = 32;
      ps.gravity.set(0, invert ? 10 : -10, 0);
      ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      ps.targetStopDuration = 0.06;
      this._launch(ps);
    };
    const rippleAt = (x, z, tint = 1) => {
      const ringEntry = this._acquireRing();
      const ring = ringEntry.mesh;
      const ringMat = ringEntry.mat;
      ring.position.set(x, 1.8, z);
      ring.scaling.set(18, 18, 18);
      ringMat.emissiveColor.set(
        Math.min(1, c.r * tint + 0.2),
        Math.min(1, c.g * tint + 0.2),
        Math.min(1, c.b * tint + 0.2),
      );

      const discEntry = this._acquireDisc();
      const disc = discEntry.mesh;
      const discMat = discEntry.mat;
      disc.position.set(x, 0.3, z);
      disc.scaling.set(8, 8, 1);
      discMat.emissiveColor.set(c.r * tint, c.g * tint, c.b * tint);
      discMat.alpha = 0.22;

      const start = performance.now();
      const obs = this.scene.onBeforeRenderObservable.add(() => {
        const t = (performance.now() - start) / 320;
        if (t >= 1) {
          this.scene.onBeforeRenderObservable.remove(obs);
          this._releaseRing(ringEntry);
          this._releaseDisc(discEntry);
          return;
        }
        const ringScale = 18 * (1 + t * 1.75);
        ring.scaling.set(ringScale, ringScale, 18);
        const discScale = 8 * (1 + t * 0.9);
        disc.scaling.set(discScale, discScale, 1);
        ringMat.alpha = 1 - t;
        discMat.alpha = Math.max(0, 0.22 - t * 0.18);
      });
    };
    const portalAt = (x, z, invert = false, tint = 1) => {
      // Shell stays un-pooled: it is a cylinder, so the unit ring/disc pools
      // cannot approximate it by scaling.
      const shell = B.MeshBuilder.CreateCylinder(`tp-shell-${++_psCounter}`, {
        height: 14,
        diameter: 12,
        tessellation: 24,
      }, this.scene);
      shell.position.set(x, 8, z);
      const shellMat = new B.StandardMaterial(`tp-shell-mat-${_psCounter}`, this.scene);
      shellMat.diffuseColor = B.Color3.Black();
      shellMat.emissiveColor = new B.Color3(
        Math.min(1, c.r * tint + 0.18),
        Math.min(1, c.g * tint + 0.18),
        Math.min(1, c.b * tint + 0.18),
      );
      shellMat.disableLighting = true;
      shellMat.alpha = 0.18;
      shell.material = shellMat;

      const innerEntry = this._acquireRing();
      const innerRing = innerEntry.mesh;
      const innerMat = innerEntry.mat;
      innerRing.position.set(x, 4.2, z);
      innerRing.scaling.set(10, 10, 10);
      innerMat.emissiveColor.set(
        Math.min(1, c.r * tint + 0.12),
        Math.min(1, c.g * tint + 0.12),
        Math.min(1, c.b * tint + 0.12),
      );
      innerMat.alpha = 0.72;

      const start = performance.now();
      const obs = this.scene.onBeforeRenderObservable.add(() => {
        const t = (performance.now() - start) / 240;
        if (t >= 1) {
          this.scene.onBeforeRenderObservable.remove(obs);
          shell.dispose();
          shellMat.dispose();
          this._releaseRing(innerEntry);
          return;
        }
        const pulse = Math.sin(t * Math.PI);
        shell.scaling.set(1 + pulse * 0.22, 0.5 + pulse * 0.85, 1 + pulse * 0.22);
        shell.position.y = 8 + (invert ? -1 : 1) * pulse * 2.4;
        innerRing.position.y = 4.2 + (invert ? -1 : 1) * pulse * 1.6;
        innerRing.rotation.y += 0.08 * (invert ? -1 : 1);
        shellMat.alpha = Math.max(0, 0.2 - t * 0.14);
        innerMat.alpha = Math.max(0, 0.8 - t * 0.72);
      });
    };

    burstAt(fromX, fromZ, true);
    burstAt(toX, toZ, false);
    rippleAt(fromX, fromZ, 0.9);
    rippleAt(toX, toZ, 1.15);
    portalAt(fromX, fromZ, true, 0.95);
    portalAt(toX, toZ, false, 1.15);
  }

  /** @private */
  _cleanup(now) {
    // Hold finished systems just past the longest tail in this file — the
    // death embers (targetStopDuration 0.15s + maxLifeTime 0.9s = 1.05s) —
    // plus a 100ms margin; everything else finishes well under that.
    this.active = this.active.filter(e => {
      if (now - e.created > 1150) { e.dispose(); return false; }
      return true;
    });
  }

  /**
   * Dispose pooled meshes/materials, any still-active transient particle
   * systems, and the shared glow texture. In-flight ring/disc animations
   * (<= ~400ms) release into the emptied pools afterward; their meshes are
   * reclaimed by scene.dispose() at teardown.
   */
  dispose() {
    for (const e of this.active) e.dispose();
    this.active = [];
    if (this._ringPool) {
      for (const { mesh, mat } of this._ringPool) { mesh.dispose(); mat.dispose(); }
      this._ringPool.length = 0;
    }
    if (this._discPool) {
      for (const { mesh, mat } of this._discPool) { mesh.dispose(); mat.dispose(); }
      this._discPool.length = 0;
    }
    if (this._dmgPool) {
      for (const { plane, tex, mat } of this._dmgPool) { plane.dispose(); tex.dispose(); mat.dispose(); }
      this._dmgPool.length = 0;
    }
    // Active-entry dispose() pushed every in-flight system back into the
    // pool above, so draining the pool here is the single place pooled
    // particle systems actually release their GPU buffers. dispose(false)
    // protects the shared glow texture.
    if (this._psPool) {
      for (const ps of this._psPool) { try { ps.dispose(false); } catch { /* scene torn down */ } }
      this._psPool.length = 0;
    }
    // Shared glow texture is freed last, after every particle system that
    // referenced it has been disposed without it (dispose(false)).
    if (this._glowTex) {
      this._glowTex.dispose();
      this._glowTex = null;
    }
  }
}
