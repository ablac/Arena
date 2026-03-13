'use strict';

/**
 * Arena environment — stone floor, boundary walls, dark void, safe zone ring.
 * @module renderer/environment
 */

import { makeMat } from './utils.js';

const GRID_SPACING = 100;
const ZONE_RING_SEGMENTS = 80;

export class EnvironmentRenderer {
  /** @param {BABYLON.Scene} scene @param {number} w @param {number} h */
  constructor(scene, w, h) {
    this.scene = scene;
    this.w = w;
    this.h = h;
    this._zoneRing = null;
    this._targetRing = null;
    this._zoneMat = null;
    this._targetMat = null;
    this._lastZoneR = -1;
    this._lastZoneCx = -1;
    this._lastZoneCy = -1;

    this._createFloor();
    this._createWalls();
    this._createVoid();
    this._initZoneMaterials();
    this._createAmbientParticles();
  }

  /** @private Procedural polished stone/metal floor with subtle grid. */
  _createFloor() {
    const B = window.BABYLON;
    const ground = B.MeshBuilder.CreateGround('ground', {
      width: this.w, height: this.h, subdivisions: 4
    }, this.scene);
    ground.position.x = this.w / 2;
    ground.position.z = this.h / 2;

    const size = 2048;
    const tex = new B.DynamicTexture('floorTex', size, this.scene, false);
    const ctx = tex.getContext();

    // Dark stone base
    ctx.fillStyle = '#1a1815';
    ctx.fillRect(0, 0, size, size);

    // Large stone-like patches for color variation
    for (let i = 0; i < 200; i++) {
      const x = Math.random() * size, y = Math.random() * size;
      const s = 30 + Math.random() * 80;
      const v = 20 + Math.floor(Math.random() * 15);
      const a = 0.15 + Math.random() * 0.2;
      ctx.fillStyle = `rgba(${v + 10},${v + 6},${v}, ${a})`;
      ctx.beginPath();
      ctx.ellipse(x, y, s, s * (0.6 + Math.random() * 0.8), Math.random() * Math.PI, 0, Math.PI * 2);
      ctx.fill();
    }

    // Smaller detail noise patches
    for (let i = 0; i < 800; i++) {
      const x = Math.random() * size, y = Math.random() * size;
      const s = 3 + Math.random() * 10;
      const v = 22 + Math.floor(Math.random() * 18);
      ctx.fillStyle = `rgba(${v + 8},${v + 4},${v}, 0.3)`;
      ctx.fillRect(x, y, s, s);
    }

    // Grid lines with blue tint (arena theme)
    const cell = size / (this.w / GRID_SPACING);
    ctx.strokeStyle = 'rgba(60, 80, 120, 0.4)';
    ctx.lineWidth = 1.5;
    for (let i = 0; i <= size; i += cell) {
      ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, size); ctx.stroke();
      ctx.beginPath(); ctx.moveTo(0, i); ctx.lineTo(size, i); ctx.stroke();
    }

    // Subtle cracks/veins between stone tiles
    ctx.strokeStyle = 'rgba(10, 8, 6, 0.5)';
    ctx.lineWidth = 1;
    for (let gx = 0; gx < size; gx += cell) {
      for (let gy = 0; gy < size; gy += cell) {
        // Random crack segments within each cell
        const numCracks = 1 + Math.floor(Math.random() * 3);
        for (let c = 0; c < numCracks; c++) {
          const sx = gx + Math.random() * cell;
          const sy = gy + Math.random() * cell;
          ctx.beginPath();
          ctx.moveTo(sx, sy);
          let cx2 = sx, cy2 = sy;
          const segs = 2 + Math.floor(Math.random() * 4);
          for (let s = 0; s < segs; s++) {
            cx2 += (Math.random() - 0.5) * 20;
            cy2 += (Math.random() - 0.5) * 20;
            ctx.lineTo(cx2, cy2);
          }
          ctx.stroke();
        }
      }
    }

    // Vignette/darkening at the edges of each grid cell
    for (let gx = 0; gx < size; gx += cell) {
      for (let gy = 0; gy < size; gy += cell) {
        const grad = ctx.createRadialGradient(
          gx + cell / 2, gy + cell / 2, cell * 0.2,
          gx + cell / 2, gy + cell / 2, cell * 0.55
        );
        grad.addColorStop(0, 'rgba(0, 0, 0, 0)');
        grad.addColorStop(1, 'rgba(0, 0, 0, 0.15)');
        ctx.fillStyle = grad;
        ctx.fillRect(gx, gy, cell, cell);
      }
    }

    tex.update();

    const mat = new B.StandardMaterial('floorMat', this.scene);
    mat.diffuseTexture = tex;
    mat.diffuseColor = new B.Color3(0.25, 0.22, 0.18);
    mat.specularColor = new B.Color3(0.05, 0.05, 0.05);
    mat.emissiveColor = new B.Color3(0.03, 0.025, 0.02);
    mat.freeze();
    ground.material = mat;
    ground.isPickable = false;
    ground.freezeWorldMatrix();
  }

  /** @private Perimeter walls. */
  _createWalls() {
    const B = window.BABYLON;
    const wallH = 30, wallD = 6;
    const wallMat = makeMat('wallMat', this.scene, new B.Color3(0.35, 0.3, 0.25), {
      emissiveFactor: 0.15, specular: new B.Color3(0.1, 0.1, 0.1), backFace: true
    });
    wallMat.freeze();
    const specs = [
      [this.w / 2, 0, this.w + wallD, wallD],
      [this.w / 2, this.h, this.w + wallD, wallD],
      [0, this.h / 2, wallD, this.h],
      [this.w, this.h / 2, wallD, this.h],
    ];
    for (let i = 0; i < specs.length; i++) {
      const [cx, cz, bw, bd] = specs[i];
      const wall = B.MeshBuilder.CreateBox(`wall-${i}`, {
        width: bw, height: wallH, depth: bd
      }, this.scene);
      wall.position.set(cx, wallH / 2, cz);
      wall.material = wallMat;
      wall.isPickable = false;
      wall.freezeWorldMatrix();
    }
  }

  /** @private Dark void outside arena. */
  _createVoid() {
    const B = window.BABYLON;
    const v = B.MeshBuilder.CreateGround('void', {
      width: this.w * 4, height: this.h * 4
    }, this.scene);
    v.position.set(this.w / 2, -0.5, this.h / 2);
    const mat = new B.StandardMaterial('voidMat', this.scene);
    mat.diffuseColor = new B.Color3(0.02, 0.02, 0.03);
    mat.specularColor = B.Color3.Black();
    mat.emissiveColor = new B.Color3(0.01, 0.01, 0.015);
    mat.freeze();
    v.material = mat;
    v.isPickable = false;
    v.freezeWorldMatrix();
  }

  /** @private Ambient floating dust/ember particles for atmosphere. */
  _createAmbientParticles() {
    const B = window.BABYLON;
    const ps = new B.ParticleSystem('ambientParticles', 25, this.scene);

    // Use default particle texture (white circle)
    ps.particleTexture = new B.Texture(
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAAAXNSR0IArs4c6QAAAYhJREFUWEftlr1OwzAQx/+XtCJiYGBhYeUFeAd4Bx6BhYGBBYmJhRdgZYCJB+AdeAcWBgYGBqQKpXHk+3DO51zSVEhYSk7+3P3u/OfMGBk/8P7r4BRAP8rAs45cM6x3W5xeXmJ6XSKLMvw+vqK+/t7LBaLHxF1AZIkQZqmODk5wXQ6RZ7n+HcA5nn+XQDHx8e4uLjAbDbDZDJBkiSfBuA7MUkSnJ2d4fj4+NMAvBPL5RJHR0c4Pz9HmqZIU38xDj+8EwDOOS4uLrBarbBer3FzcyMAXq6usFgssFqtsFwuP13YNwqYc47tdovtdou7uzu8vLyIAO/v7/jMAnQB/kcR/IzjUQB8DPhnAbiUA8DhwQEm4zEODw9FmcP8fr+Pz+czTCYT7O3t4e3tTfws+z3BNADYH2Ecx9jZ2cHm5qY4S9NYBOj3+5hOp9jY2EB5fY3ZbIbFYiHugTAMxWeA0WiE4XCIwWCA4XCI8XiMbrcr7oHJZIJ+v4/RaITxeIx+vy/+DYeD/yvy7wC+ADaOYCHhWiMeAAAAAElFTkSuQmCC',
      this.scene
    );

    // Box emitter covering the arena center area
    const emitW = Math.min(this.w, 1500) * 0.5;
    const emitH = Math.min(this.h, 1500) * 0.5;
    ps.emitter = new B.Vector3(this.w / 2, 5, this.h / 2);
    ps.minEmitBox = new B.Vector3(-emitW, 0, -emitH);
    ps.maxEmitBox = new B.Vector3(emitW, 40, emitH);

    // Small particles
    ps.minSize = 0.5;
    ps.maxSize = 1.5;

    // Slow upward movement
    ps.minEmitPower = 1;
    ps.maxEmitPower = 3;
    ps.direction1 = new B.Vector3(-0.3, 1, -0.3);
    ps.direction2 = new B.Vector3(0.3, 1, 0.3);

    // Long lifetime
    ps.minLifeTime = 3;
    ps.maxLifeTime = 6;

    // Emission rate — slow and sparse
    ps.emitRate = 5;

    // Warm amber/orange fading to transparent
    ps.color1 = new B.Color4(1.0, 0.7, 0.3, 0.4);
    ps.color2 = new B.Color4(1.0, 0.5, 0.2, 0.2);
    ps.colorDead = new B.Color4(1.0, 0.3, 0.1, 0.0);

    // Additive blending for glow effect
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;

    // Slight gravity pull to slow upward drift
    ps.gravity = new B.Vector3(0, -0.5, 0);

    ps.start();
    this._ambientParticles = ps;
  }

  /** @private Create reusable materials for zone rings. */
  _initZoneMaterials() {
    const B = window.BABYLON;
    // Current zone boundary — electric blue
    this._zoneMat = new B.StandardMaterial('zoneRingMat', this.scene);
    this._zoneMat.emissiveColor = new B.Color3(0.1, 0.5, 1.0);
    this._zoneMat.diffuseColor = new B.Color3(0, 0, 0);
    this._zoneMat.specularColor = B.Color3.Black();
    this._zoneMat.disableLighting = true;
    this._zoneMat.alpha = 0.7;
    this._zoneMat.backFaceCulling = false;

    // Target zone — dim white
    this._targetMat = new B.StandardMaterial('targetRingMat', this.scene);
    this._targetMat.emissiveColor = new B.Color3(1.0, 1.0, 1.0);
    this._targetMat.diffuseColor = new B.Color3(0, 0, 0);
    this._targetMat.specularColor = B.Color3.Black();
    this._targetMat.disableLighting = true;
    this._targetMat.alpha = 0.25;
    this._targetMat.backFaceCulling = false;
  }

  /**
   * Update safe zone visualization.
   * @param {Object|null} safeZone - { center: [x,y], radius, target_center: [x,y], target_radius }
   */
  update(safeZone) {
    if (!safeZone) return;

    const cx = safeZone.center[0];
    const cy = safeZone.center[1];
    const r = safeZone.radius;

    // Only rebuild rings when zone has actually changed
    if (Math.abs(this._lastZoneR - r) < 0.5 &&
        Math.abs(this._lastZoneCx - cx) < 0.5 &&
        Math.abs(this._lastZoneCy - cy) < 0.5) {
      return;
    }
    this._lastZoneR = r;
    this._lastZoneCx = cx;
    this._lastZoneCy = cy;

    this._buildZoneRing(cx, cy, r);
    if (safeZone.target_center) {
      this._buildTargetRing(
        safeZone.target_center[0], safeZone.target_center[1],
        safeZone.target_radius || 75
      );
    }
  }

  /** @private Build or rebuild the current zone boundary ring. */
  _buildZoneRing(cx, cy, r) {
    if (this._zoneRing) this._zoneRing.dispose();
    const B = window.BABYLON;
    const thickness = Math.max(3, r * 0.005);
    this._zoneRing = B.MeshBuilder.CreateTorus('zoneRing', {
      diameter: r * 2, thickness, tessellation: ZONE_RING_SEGMENTS
    }, this.scene);
    this._zoneRing.rotation.x = 0; // flat on ground
    this._zoneRing.position.set(cx, 2, cy);
    this._zoneRing.material = this._zoneMat;
  }

  /** @private Build or rebuild the target zone ring. */
  _buildTargetRing(cx, cy, r) {
    if (this._targetRing) this._targetRing.dispose();
    const B = window.BABYLON;
    this._targetRing = B.MeshBuilder.CreateTorus('targetRing', {
      diameter: r * 2, thickness: 2, tessellation: 48
    }, this.scene);
    this._targetRing.position.set(cx, 1, cy);
    this._targetRing.material = this._targetMat;
  }
}
