'use strict';

/**
 * Renderer for new gameplay features: teleport pads, hazard zones,
 * landmines, gravity wells, void tiles, bounty markers.
 * @module renderer/gameplay
 */

import { makeMat, parseColor } from './utils.js';

export class GameplayRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, object>} */
    this.teleportPads = new Map();
    /** @type {Map<string, object>} */
    this.hazardZones = new Map();
    /** @type {Map<string, BABYLON.Mesh>} */
    this.landmines = new Map();
    /** @type {Map<string, object>} */
    this.gravityWells = new Map();
    /** @type {Map<string, BABYLON.Mesh>} */
    this.voidTiles = new Map();
    /** @type {object|null} */
    this.bountyGroup = null;
    this._tick = 0;
    this._glowTex = null;
    this._initGlowTexture();
  }

  /** @private Create shared glow particle texture. */
  _initGlowTexture() {
    const B = window.BABYLON;
    this._glowTex = new B.DynamicTexture('gpGlowTex', 32, this.scene, false);
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

  update(state) {
    this._tick++;
    this._updateTeleportPads(state.teleport_pads || []);
    this._updateHazardZones(state.hazard_zones || []);
    this._updateLandmines(state.landmines || []);
    this._updateGravityWells(state.gravity_wells || []);
    this._updateVoidTiles(state.void_tiles || []);
    this._updateBounty(state.bots || [], state.bounty_target);
  }

  // ═══════════════════════════════════════════════════════════════════
  // TELEPORT PADS — glowing platform + beam column + particles
  // ═══════════════════════════════════════════════════════════════════
  _updateTeleportPads(pads) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const pad of pads) {
      seen.add(pad.id);
      let entry = this.teleportPads.get(pad.id);
      if (!entry || entry.platform.isDisposed()) {
        const color = parseColor(pad.color || '#00ffff');

        // Platform disc
        const platform = B.MeshBuilder.CreateCylinder(`tp-${pad.id}`, {
          height: 1.5, diameter: 24, tessellation: 32,
        }, this.scene);
        const platMat = new B.StandardMaterial(`tp-plat-${pad.id}`, this.scene);
        platMat.diffuseColor = B.Color3.Black();
        platMat.emissiveColor = color;
        platMat.alpha = 0.5;
        platMat.disableLighting = true;
        platMat.backFaceCulling = false;
        platform.material = platMat;
        platform.isPickable = false;

        // Inner ring
        const ring = B.MeshBuilder.CreateTorus(`tp-ring-${pad.id}`, {
          diameter: 22, thickness: 1.2, tessellation: 32,
        }, this.scene);
        const ringMat = new B.StandardMaterial(`tp-ring-mat-${pad.id}`, this.scene);
        ringMat.diffuseColor = B.Color3.Black();
        ringMat.emissiveColor = color;
        ringMat.disableLighting = true;
        ringMat.alpha = 0.8;
        ring.material = ringMat;
        ring.isPickable = false;

        // Vertical beam column
        const beam = new B.ParticleSystem(`tp-beam-${pad.id}`, 40, this.scene);
        beam.particleTexture = this._glowTex;
        beam.emitter = new B.Vector3(0, 1, 0);
        beam.minEmitBox = new B.Vector3(-3, 0, -3);
        beam.maxEmitBox = new B.Vector3(3, 0, 3);
        beam.direction1 = new B.Vector3(-0.1, 1, -0.1);
        beam.direction2 = new B.Vector3(0.1, 1, 0.1);
        beam.minEmitPower = 15;
        beam.maxEmitPower = 30;
        beam.minLifeTime = 0.8;
        beam.maxLifeTime = 1.5;
        beam.minSize = 4;
        beam.maxSize = 10;
        beam.emitRate = 20;
        beam.gravity = new B.Vector3(0, 5, 0);
        beam.color1 = new B.Color4(color.r, color.g, color.b, 0.7);
        beam.color2 = new B.Color4(color.r * 0.5, color.g * 0.5, color.b * 0.5, 0.3);
        beam.colorDead = new B.Color4(color.r * 0.2, color.g * 0.2, color.b * 0.2, 0);
        beam.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        beam.start();

        // Swirl particles around the base
        const swirl = new B.ParticleSystem(`tp-swirl-${pad.id}`, 25, this.scene);
        swirl.particleTexture = this._glowTex;
        swirl.emitter = new B.Vector3(0, 2, 0);
        swirl.createCylinderEmitter(10, 1, 0, 0);
        swirl.minLifeTime = 1.0;
        swirl.maxLifeTime = 2.0;
        swirl.minSize = 2;
        swirl.maxSize = 5;
        swirl.emitRate = 12;
        swirl.minEmitPower = 2;
        swirl.maxEmitPower = 5;
        swirl.color1 = new B.Color4(color.r, color.g, color.b, 0.5);
        swirl.color2 = new B.Color4(1, 1, 1, 0.2);
        swirl.colorDead = new B.Color4(color.r, color.g, color.b, 0);
        swirl.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        swirl.start();

        entry = { platform, ring, beam, swirl, color };
        this.teleportPads.set(pad.id, entry);
      }

      const pos = pad.position;
      const x = pos[0], z = pos[1];
      entry.platform.position.set(x, 0.75, z);
      entry.ring.position.set(x, 2 + Math.sin(this._tick * 0.04) * 0.5, z);
      entry.ring.rotation.y += 0.02;
      entry.beam.emitter.x = x;
      entry.beam.emitter.z = z;
      entry.swirl.emitter.x = x;
      entry.swirl.emitter.z = z;

      // Pulse the platform glow
      entry.platform.material.alpha = 0.4 + 0.15 * Math.sin(this._tick * 0.06);
    }

    for (const [id, entry] of this.teleportPads) {
      if (!seen.has(id)) {
        entry.platform.dispose();
        entry.ring.dispose();
        entry.beam.dispose();
        entry.swirl.dispose();
        this.teleportPads.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // HAZARD ZONES — electric floor panels with spark particles
  // ═══════════════════════════════════════════════════════════════════
  _updateHazardZones(zones) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const zone of zones) {
      seen.add(zone.id);
      let entry = this.hazardZones.get(zone.id);
      if (!entry || (entry.edges[0] && entry.edges[0].isDisposed())) {
        const w = (zone.width || 3) * 20;
        const h = (zone.height || 3) * 20;

        // Red outline using 4 thin box edges (no filled plane)
        const thick = 1.0;
        const edgeMat = new B.StandardMaterial(`hz-edge-${zone.id}`, this.scene);
        edgeMat.diffuseColor = B.Color3.Black();
        edgeMat.emissiveColor = new B.Color3(1.0, 0.1, 0.05);
        edgeMat.disableLighting = true;
        edgeMat.alpha = 0.7;

        const parent = new B.TransformNode(`hz-parent-${zone.id}`, this.scene);
        const edges = [];
        // Top edge (along X)
        const top = B.MeshBuilder.CreateBox(`hz-t-${zone.id}`, { width: w, height: thick, depth: thick }, this.scene);
        top.position.set(0, 0, h / 2); top.material = edgeMat; top.parent = parent; top.isPickable = false; edges.push(top);
        // Bottom edge
        const bot = B.MeshBuilder.CreateBox(`hz-b-${zone.id}`, { width: w, height: thick, depth: thick }, this.scene);
        bot.position.set(0, 0, -h / 2); bot.material = edgeMat; bot.parent = parent; bot.isPickable = false; edges.push(bot);
        // Left edge (along Z)
        const left = B.MeshBuilder.CreateBox(`hz-l-${zone.id}`, { width: thick, height: thick, depth: h }, this.scene);
        left.position.set(-w / 2, 0, 0); left.material = edgeMat; left.parent = parent; left.isPickable = false; edges.push(left);
        // Right edge
        const right = B.MeshBuilder.CreateBox(`hz-r-${zone.id}`, { width: thick, height: thick, depth: h }, this.scene);
        right.position.set(w / 2, 0, 0); right.material = edgeMat; right.parent = parent; right.isPickable = false; edges.push(right);

        // Electric zap particles — arc-like bolts inside the zone
        const zaps = new B.ParticleSystem(`hz-zap-${zone.id}`, 50, this.scene);
        zaps.particleTexture = this._glowTex;
        zaps.emitter = new B.Vector3(0, 2, 0);
        zaps.minEmitBox = new B.Vector3(-w / 2 + 5, 0, -h / 2 + 5);
        zaps.maxEmitBox = new B.Vector3(w / 2 - 5, 0, h / 2 - 5);
        // Zaps shoot sideways and up — mimics arcing electricity
        zaps.direction1 = new B.Vector3(-8, 2, -8);
        zaps.direction2 = new B.Vector3(8, 10, 8);
        zaps.minEmitPower = 8;
        zaps.maxEmitPower = 25;
        zaps.minLifeTime = 0.05;
        zaps.maxLifeTime = 0.15;
        zaps.minSize = 0.5;
        zaps.maxSize = 2.5;
        zaps.emitRate = 0;
        zaps.gravity = new B.Vector3(0, -30, 0);
        zaps.color1 = new B.Color4(0.8, 0.9, 1.0, 1.0);
        zaps.color2 = new B.Color4(0.4, 0.6, 1.0, 0.8);
        zaps.colorDead = new B.Color4(0.2, 0.2, 0.8, 0);
        zaps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        zaps.start();

        // Bigger spark bursts
        const sparks = new B.ParticleSystem(`hz-sparks-${zone.id}`, 20, this.scene);
        sparks.particleTexture = this._glowTex;
        sparks.emitter = new B.Vector3(0, 1, 0);
        sparks.minEmitBox = new B.Vector3(-w / 2, 0, -h / 2);
        sparks.maxEmitBox = new B.Vector3(w / 2, 0, h / 2);
        sparks.direction1 = new B.Vector3(-2, 5, -2);
        sparks.direction2 = new B.Vector3(2, 15, 2);
        sparks.minEmitPower = 3;
        sparks.maxEmitPower = 12;
        sparks.minLifeTime = 0.1;
        sparks.maxLifeTime = 0.3;
        sparks.minSize = 1;
        sparks.maxSize = 4;
        sparks.emitRate = 0;
        sparks.gravity = new B.Vector3(0, -40, 0);
        sparks.color1 = new B.Color4(1.0, 0.7, 0.2, 0.9);
        sparks.color2 = new B.Color4(1.0, 0.3, 0.0, 0.6);
        sparks.colorDead = new B.Color4(0.5, 0.1, 0.0, 0);
        sparks.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        sparks.start();

        entry = { parent, edges, edgeMat, zaps, sparks, w, h };
        this.hazardZones.set(zone.id, entry);
      }

      const pos = zone.position;
      entry.parent.position.set(pos[0], 1.0, pos[1]);

      if (zone.active) {
        // Bright red pulsing outline + electrical zapping
        const pulse = 0.7 + 0.3 * Math.sin(this._tick * 0.2);
        entry.edgeMat.emissiveColor.set(1.0, 0.15 * pulse, 0.05);
        entry.edgeMat.alpha = pulse;
        entry.zaps.emitRate = 40;
        entry.sparks.emitRate = 15;
      } else {
        // Dim outline, no electricity
        entry.edgeMat.emissiveColor.set(0.3, 0.05, 0.02);
        entry.edgeMat.alpha = 0.25;
        entry.zaps.emitRate = 0;
        entry.sparks.emitRate = 0;
      }

      entry.zaps.emitter.x = pos[0];
      entry.zaps.emitter.z = pos[1];
      entry.sparks.emitter.x = pos[0];
      entry.sparks.emitter.z = pos[1];
    }

    for (const [id, entry] of this.hazardZones) {
      if (!seen.has(id)) {
        entry.edges.forEach(e => e.dispose());
        entry.parent.dispose();
        entry.zaps.dispose();
        entry.sparks.dispose();
        this.hazardZones.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // LANDMINES — small blinking devices
  // ═══════════════════════════════════════════════════════════════════
  _updateLandmines(mines) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const mine of mines) {
      seen.add(mine.id);
      let mesh = this.landmines.get(mine.id);
      if (!mesh || mesh.isDisposed()) {
        mesh = B.MeshBuilder.CreateCylinder(`mine-${mine.id}`, {
          height: 1.5, diameter: 6, tessellation: 8,
        }, this.scene);
        const mat = new B.StandardMaterial(`mine-mat-${mine.id}`, this.scene);
        mat.diffuseColor = new B.Color3(0.2, 0.2, 0.2);
        mat.emissiveColor = new B.Color3(0.3, 0.05, 0.05);
        mat.disableLighting = true;
        mesh.material = mat;
        mesh.isPickable = false;
        this.landmines.set(mine.id, mesh);
      }
      const pos = mine.position;
      mesh.position.set(pos[0], 0.75, pos[1]);
      // Blink red light when armed
      if (mine.armed) {
        const blink = Math.sin(this._tick * 0.3) > 0 ? 0.9 : 0.2;
        mesh.material.emissiveColor.set(blink, 0.05, 0.05);
      } else {
        mesh.material.emissiveColor.set(0.1, 0.1, 0.05);
      }
    }

    for (const [id, mesh] of this.landmines) {
      if (!seen.has(id)) {
        mesh.dispose();
        this.landmines.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // GRAVITY WELLS — spinning vortex with inward particles
  // ═══════════════════════════════════════════════════════════════════
  _updateGravityWells(wells) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const well of wells) {
      seen.add(well.id);
      let entry = this.gravityWells.get(well.id);
      if (!entry || entry.ring.isDisposed()) {
        const pullR = (well.pull_radius || 3) * 20;

        // Spinning torus
        const ring = B.MeshBuilder.CreateTorus(`gw-${well.id}`, {
          diameter: pullR, thickness: 3, tessellation: 32,
        }, this.scene);
        const ringMat = new B.StandardMaterial(`gw-mat-${well.id}`, this.scene);
        ringMat.diffuseColor = B.Color3.Black();
        ringMat.emissiveColor = new B.Color3(0.4, 0.0, 0.9);
        ringMat.alpha = 0.6;
        ringMat.disableLighting = true;
        ringMat.backFaceCulling = false;
        ring.material = ringMat;
        ring.isPickable = false;

        // Inner ring
        const inner = B.MeshBuilder.CreateTorus(`gw-inner-${well.id}`, {
          diameter: pullR * 0.5, thickness: 2, tessellation: 24,
        }, this.scene);
        inner.material = ringMat;
        inner.isPickable = false;

        // Inward-pulling particle vortex
        const vortex = new B.ParticleSystem(`gw-vortex-${well.id}`, 60, this.scene);
        vortex.particleTexture = this._glowTex;
        vortex.emitter = new B.Vector3(0, 3, 0);
        vortex.createCylinderEmitter(pullR / 2, 3, 0, 0);
        vortex.minLifeTime = 0.5;
        vortex.maxLifeTime = 1.2;
        vortex.minSize = 2;
        vortex.maxSize = 6;
        vortex.emitRate = 40;
        vortex.minEmitPower = -10; // negative = pull inward
        vortex.maxEmitPower = -5;
        vortex.gravity = new B.Vector3(0, -3, 0);
        vortex.color1 = new B.Color4(0.5, 0.0, 1.0, 0.8);
        vortex.color2 = new B.Color4(0.2, 0.0, 0.6, 0.4);
        vortex.colorDead = new B.Color4(0.1, 0.0, 0.3, 0);
        vortex.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        vortex.start();

        entry = { ring, inner, vortex, pullR };
        this.gravityWells.set(well.id, entry);
      }

      const pos = well.position;
      const x = pos[0], z = pos[1];
      entry.ring.position.set(x, 3, z);
      entry.ring.rotation.y += 0.06;
      entry.ring.rotation.x = Math.sin(this._tick * 0.03) * 0.3;
      entry.inner.position.set(x, 5, z);
      entry.inner.rotation.y -= 0.1;
      entry.vortex.emitter.x = x;
      entry.vortex.emitter.z = z;
      // Pulsing alpha
      entry.ring.material.alpha = 0.4 + 0.3 * Math.sin(this._tick * 0.08);
    }

    for (const [id, entry] of this.gravityWells) {
      if (!seen.has(id)) {
        entry.ring.dispose();
        entry.inner.dispose();
        entry.vortex.dispose();
        this.gravityWells.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // VOID TILES — sudden death crumbling floor
  // ═══════════════════════════════════════════════════════════════════
  _updateVoidTiles(tiles) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const tile of tiles) {
      const key = `${tile[0]}_${tile[1]}`;
      seen.add(key);
      if (this.voidTiles.has(key)) continue;

      const mesh = B.MeshBuilder.CreatePlane(`void-${key}`, { size: 20 }, this.scene);
      mesh.rotation.x = Math.PI / 2;
      mesh.position.x = tile[0] * 20 + 10;
      mesh.position.y = 0.1;
      mesh.position.z = tile[1] * 20 + 10;
      const mat = new B.StandardMaterial(`void-mat-${key}`, this.scene);
      mat.diffuseColor = B.Color3.Black();
      mat.emissiveColor = new B.Color3(0.15, 0.0, 0.0);
      mat.alpha = 0.85;
      mat.disableLighting = true;
      mesh.material = mat;
      mesh.isPickable = false;
      this.voidTiles.set(key, mesh);
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // BOUNTY — golden spinning crown ring above the target
  // ═══════════════════════════════════════════════════════════════════
  _updateBounty(bots, bountyTargetId) {
    const B = window.BABYLON;
    if (!bountyTargetId) {
      if (this.bountyGroup) this.bountyGroup.ring.visibility = 0;
      return;
    }

    const target = bots.find(b => b.bot_id === bountyTargetId || b.id === bountyTargetId);
    if (!target || !target.is_alive) {
      if (this.bountyGroup) this.bountyGroup.ring.visibility = 0;
      return;
    }

    if (!this.bountyGroup || this.bountyGroup.ring.isDisposed()) {
      const ring = B.MeshBuilder.CreateTorus('bountyRing', {
        diameter: 20, thickness: 1.5, tessellation: 32,
      }, this.scene);
      const mat = new B.StandardMaterial('bountyMat', this.scene);
      mat.diffuseColor = B.Color3.Black();
      mat.emissiveColor = new B.Color3(1.0, 0.85, 0.0);
      mat.alpha = 0.85;
      mat.disableLighting = true;
      ring.material = mat;
      ring.isPickable = false;
      this.bountyGroup = { ring, curX: 0, curZ: 0, initialized: false };
    }

    const pos = target.position;
    const tx = pos[0], tz = pos[1];
    const g = this.bountyGroup;
    if (!g.initialized) {
      g.curX = tx;
      g.curZ = tz;
      g.initialized = true;
    }
    // Smooth lerp to target position (high factor = nearly instant, no trailing)
    const lerp = 0.5;
    g.curX += (tx - g.curX) * lerp;
    g.curZ += (tz - g.curZ) * lerp;
    g.ring.position.set(g.curX, 25 + Math.sin(this._tick * 0.06) * 3, g.curZ);
    g.ring.rotation.y += 0.04;
    g.ring.visibility = 1;
  }

  dispose() {
    for (const [, e] of this.teleportPads) {
      e.platform.dispose(); e.ring.dispose(); e.beam.dispose(); e.swirl.dispose();
    }
    for (const [, e] of this.hazardZones) { e.edges.forEach(m => m.dispose()); e.parent.dispose(); e.zaps.dispose(); e.sparks.dispose(); }
    for (const m of this.landmines.values()) m.dispose();
    for (const [, e] of this.gravityWells) { e.ring.dispose(); e.inner.dispose(); e.vortex.dispose(); }
    for (const m of this.voidTiles.values()) m.dispose();
    if (this.bountyGroup) this.bountyGroup.ring.dispose();
    this.teleportPads.clear();
    this.hazardZones.clear();
    this.landmines.clear();
    this.gravityWells.clear();
    this.voidTiles.clear();
  }
}
