'use strict';

/**
 * Renderer for new gameplay features: teleport pads, hazard zones,
 * landmines, gravity wells, void tiles, bounty markers.
 * @module renderer/gameplay
 */

import { makeMat, parseColor } from './utils.js';
import { isEnabled } from '../settings.js';

export class GameplayRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, object>} */
    this.teleportPads = new Map();
    /** @type {Map<string, object>} */
    this.capturePads = new Map();
    /** @type {Map<string, object>} */
    this.hazardZones = new Map();
    /** @type {Map<string, BABYLON.Mesh>} */
    this.landmines = new Map();
    /** @type {Map<string, object>} */
    this.gravityWells = new Map();
    /** @type {Map<string, object>} */
    this.staffImpacts = new Map();
    /** @type {Map<string, object>} */
    this.burnFields = new Map();
    /** @type {Map<string, BABYLON.Mesh>} */
    this.voidTiles = new Map();
    /** @type {object|null} */
    this.bountyGroup = null;
    this.bountyTargetId = null;
    this.bountyBots = [];
    this._tick = 0;
    this._glowTex = null;
    this.onStaffImpactCreated = null;
    this._initGlowTexture();

    // Fixed capture-pad tint constants, cached once instead of allocated every tick per pad.
    const B = window.BABYLON;
    this._capturePadNeutral = new B.Color3(0.44, 0.62, 0.74);
    this._capturePadLocked = new B.Color3(0.36, 0.36, 0.4);
    this._capturePadContested = new B.Color3(1.0, 0.42, 0.2);
    // Inactive pads stay unmistakably dark for their entire lock interval.
    // Readiness is binary server state; a gradual color ramp made a locked
    // pad look usable before it actually re-armed.
    this._tpInactive = new B.Color3(0.025, 0.03, 0.04);
    this._scratchA = new B.Color3();
    this._scratchB = new B.Color3();
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
    // NOTE: this._tick advances per-frame in animate() (dt-based) so ambient
    // pulses stay smooth; update() only applies server-driven state at 10Hz.
    this._updateTeleportPads(state.teleport_pads || []);
    this._updateCapturePads(state.capture_pads || []);
    this._updateHazardZones(state.hazard_zones || []);
    this._updateBurnFields(state.burn_fields || []);
    this._updateStaffImpacts(state.staff_impacts || []);
    this._updateLandmines(state.landmines || []);
    this._updateGravityWells(state.gravity_wells || []);
    // void_tiles arrives only on keyframes now: null/undefined means "no
    // update this frame" (keep the current set); an actual array — including
    // [] after a round reset — is authoritative.
    if (Array.isArray(state.void_tiles)) this._updateVoidTiles(state.void_tiles);
    this._updateFlags(state.flags || []);
    this.bountyBots = state.bots || [];
    // Issue #13: only adopt the server's explicit target when it sends one.
    // When bounty_target is empty, keep the current holder so the kill-streak
    // heuristic in _updateBounty retains its incumbent instead of re-deriving
    // (and potentially flapping) every update. A dead incumbent is released
    // by the alive check in _updateBounty.
    if (state.bounty_target) {
      this.bountyTargetId = state.bounty_target;
    }
    this._updateBounty();
  }

  // ═══════════════════════════════════════════════════════════════════
  // CTF FLAGS — team-colored pole + banner at the base, follows carriers
  // ═══════════════════════════════════════════════════════════════════
  /** @private Team palette shared by flags (team 1..n). */
  _teamColor(team) {
    const B = window.BABYLON;
    const palette = [
      new B.Color3(0.25, 0.55, 1.0),  // team 1 — blue
      new B.Color3(1.0, 0.3, 0.25),   // team 2 — red
      new B.Color3(0.3, 0.9, 0.4),    // team 3 — green
      new B.Color3(1.0, 0.85, 0.2),   // team 4 — yellow
    ];
    return palette[(team - 1) % palette.length] || palette[0];
  }

  _updateFlags(flags) {
    const B = window.BABYLON;
    if (!this.flags) this.flags = new Map();
    const seen = new Set();

    for (const flag of flags) {
      seen.add(flag.id);
      let entry = this.flags.get(flag.id);
      if (!entry || entry.pole.isDisposed()) {
        const color = this._teamColor(flag.team || 1);

        const pole = B.MeshBuilder.CreateCylinder(`flag-pole-${flag.id}`, {
          height: 26, diameter: 1.2, tessellation: 8,
        }, this.scene);
        const poleMat = new B.StandardMaterial(`flag-pole-mat-${flag.id}`, this.scene);
        poleMat.diffuseColor = new B.Color3(0.7, 0.7, 0.75);
        poleMat.emissiveColor = color.scale(0.15);
        pole.material = poleMat;
        pole.isPickable = false;

        const banner = B.MeshBuilder.CreatePlane(`flag-banner-${flag.id}`, {
          width: 12, height: 8,
        }, this.scene);
        const bannerMat = new B.StandardMaterial(`flag-banner-mat-${flag.id}`, this.scene);
        bannerMat.diffuseColor = B.Color3.Black();
        bannerMat.emissiveColor = color;
        bannerMat.disableLighting = true;
        bannerMat.backFaceCulling = false;
        bannerMat.alpha = 0.9;
        banner.material = bannerMat;
        banner.isPickable = false;
        banner.parent = pole;
        banner.position.set(6, 9, 0);

        // Base ring marking the flag's home position.
        const base = B.MeshBuilder.CreateTorus(`flag-base-${flag.id}`, {
          diameter: 30, thickness: 1.5, tessellation: 48,
        }, this.scene);
        const baseMat = new B.StandardMaterial(`flag-base-mat-${flag.id}`, this.scene);
        baseMat.diffuseColor = B.Color3.Black();
        baseMat.emissiveColor = color.scale(0.7);
        baseMat.disableLighting = true;
        baseMat.alpha = 0.6;
        base.material = baseMat;
        base.isPickable = false;

        // Comet trail for a carried flag - CTF's key spectator moment. Idle
        // at emitRate 0; the per-frame flag block turns it on while carried.
        // Shares _glowTex, so dispose with dispose(false). 2-4 flags max.
        const trail = new B.ParticleSystem(`flag-trail-${flag.id}`, 40, this.scene);
        trail.particleTexture = this._glowTex;
        trail.emitter = new B.Vector3(flag.position[0], 14, flag.position[1]);
        trail.createPointEmitter(new B.Vector3(-0.3, 0.4, -0.3), new B.Vector3(0.3, 1, 0.3));
        trail.color1 = new B.Color4(color.r, color.g, color.b, 0.9);
        trail.color2 = new B.Color4(Math.min(1, color.r + 0.3), Math.min(1, color.g + 0.3), Math.min(1, color.b + 0.3), 0.7);
        trail.colorDead = new B.Color4(color.r * 0.3, color.g * 0.3, color.b * 0.3, 0);
        trail.minSize = 3; trail.maxSize = 7;
        trail.minLifeTime = 0.5; trail.maxLifeTime = 1.0;
        trail.minEmitPower = 2; trail.maxEmitPower = 6;
        trail.gravity = new B.Vector3(0, 4, 0);
        trail.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        trail.emitRate = 0;
        trail.start();

        entry = { pole, banner, bannerMat, base, trail, curX: flag.position[0], curZ: flag.position[1] };
        this.flags.set(flag.id, entry);
      }

      // Base ring stays at home; pole position/pulse animate per-frame in
      // _animateAmbient from the stored target.
      entry.base.position.set(flag.base_position[0], 1.5, flag.base_position[1]);
      entry.tx = flag.position[0];
      entry.tz = flag.position[1];
      entry.status = flag.status;
    }

    for (const [id, entry] of this.flags) {
      if (!seen.has(id)) {
        entry.pole.dispose(); // banner is parented and disposed with it
        entry.base.dispose();
        if (entry.trail) entry.trail.dispose(false); // shared _glowTex stays
        this.flags.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // TELEPORT PADS — glowing platform + soft portal well + particles
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

        // Vertical portal mist
        const beam = new B.ParticleSystem(`tp-beam-${pad.id}`, 40, this.scene);
        beam.particleTexture = this._glowTex;
        beam.emitter = new B.Vector3(0, 2.2, 0);
        beam.minEmitBox = new B.Vector3(-2.4, 0, -2.4);
        beam.maxEmitBox = new B.Vector3(2.4, 0, 2.4);
        beam.direction1 = new B.Vector3(-0.08, 0.18, -0.08);
        beam.direction2 = new B.Vector3(0.08, 0.55, 0.08);
        beam.minEmitPower = 2;
        beam.maxEmitPower = 7;
        beam.minLifeTime = 0.7;
        beam.maxLifeTime = 1.25;
        beam.minSize = 5;
        beam.maxSize = 11;
        beam.emitRate = 16;
        beam.gravity = new B.Vector3(0, 1.5, 0);
        beam.color1 = new B.Color4(color.r, color.g, color.b, 0.4);
        beam.color2 = new B.Color4(color.r * 0.45, color.g * 0.45, color.b * 0.45, 0.18);
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

        const halo = B.MeshBuilder.CreateDisc(`tp-halo-${pad.id}`, {
          radius: 9.5,
          tessellation: 34,
        }, this.scene);
        halo.rotation.x = Math.PI / 2;
        halo.position.y = 0.18;
        halo.isPickable = false;
        const haloMat = new B.StandardMaterial(`tp-halo-mat-${pad.id}`, this.scene);
        haloMat.diffuseColor = B.Color3.Black();
        haloMat.emissiveColor = color.scale(0.95);
        haloMat.disableLighting = true;
        haloMat.alpha = 0.16;
        halo.material = haloMat;

        entry = { platform, ring, beam, swirl, halo, haloMat, color };
        this.teleportPads.set(pad.id, entry);
      }

      const pos = pad.position;
      const x = pos[0], z = pos[1];
      const ready = pad.is_ready !== false;
      const blend = ready ? 1 : 0;
      if (!ready && entry.ready !== false) {
        entry.beam.stop();
        entry.swirl.stop();
        if (entry.beam.reset) entry.beam.reset();
        if (entry.swirl.reset) entry.swirl.reset();
      } else if (ready && entry.ready === false) {
        entry.beam.start();
        entry.swirl.start();
      }
      // Lerp straight into the platform material's existing Color3, then fan
      // out with copyFrom — no per-tick Color3 allocations.
      const emissive = entry.platform.material.emissiveColor;
      B.Color3.LerpToRef(this._tpInactive, entry.color, blend, emissive);
      entry.platform.position.set(x, 0.75, z);
      entry.beam.emitter.x = x;
      entry.beam.emitter.z = z;
      entry.swirl.emitter.x = x;
      entry.swirl.emitter.z = z;
      entry.halo.position.set(x, 0.18, z);
      entry.ring.material.emissiveColor.copyFrom(emissive);
      entry.haloMat.emissiveColor.copyFrom(emissive);
      entry.beam.color1.set(emissive.r, emissive.g, emissive.b, ready ? 0.4 : 0);
      entry.beam.color2.set(emissive.r * 0.5, emissive.g * 0.5, emissive.b * 0.5, ready ? 0.16 : 0);
      entry.swirl.color1.set(emissive.r, emissive.g, emissive.b, ready ? 0.5 : 0);
      entry.beam.emitRate = ready ? 16 : 0;
      entry.swirl.emitRate = ready ? 12 : 0;
      entry.ring.material.alpha = ready ? 0.8 : 0.08;

      // Ambient bob/pulse runs per-frame in _animateAmbient — store state.
      entry.x = x;
      entry.z = z;
      entry.ready = ready;
    }

    for (const [id, entry] of this.teleportPads) {
      if (!seen.has(id)) {
        entry.platform.dispose();
        entry.ring.dispose();
        // false: beam/swirl share _glowTex with other effect types; disposing
        // the texture here would blank out every other glow particle system.
        entry.beam.dispose(false);
        entry.swirl.dispose(false);
        entry.halo.dispose();
        entry.haloMat.dispose();
        this.teleportPads.delete(id);
      }
    }
  }

  _updateCapturePads(pads) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const pad of pads) {
      seen.add(pad.id);
      let entry = this.capturePads.get(pad.id);
      if (!entry || entry.base.isDisposed()) {
        const base = B.MeshBuilder.CreateCylinder(`cp-${pad.id}`, {
          height: 1.3, diameter: 26, tessellation: 36,
        }, this.scene);
        base.position.y = 0.7;
        base.isPickable = false;
        const baseMat = new B.StandardMaterial(`cp-mat-${pad.id}`, this.scene);
        baseMat.diffuseColor = B.Color3.Black();
        baseMat.emissiveColor = new B.Color3(0.24, 0.34, 0.42);
        baseMat.disableLighting = true;
        baseMat.alpha = 0.42;
        base.material = baseMat;

        const ring = B.MeshBuilder.CreateTorus(`cp-ring-${pad.id}`, {
          diameter: 24,
          thickness: 1.3,
          tessellation: 36,
        }, this.scene);
        ring.rotation.x = Math.PI / 2;
        ring.position.y = 1.8;
        ring.isPickable = false;
        const ringMat = new B.StandardMaterial(`cp-ring-mat-${pad.id}`, this.scene);
        ringMat.diffuseColor = B.Color3.Black();
        ringMat.emissiveColor = new B.Color3(0.45, 0.65, 0.78);
        ringMat.disableLighting = true;
        ring.material = ringMat;

        const inner = B.MeshBuilder.CreateDisc(`cp-inner-${pad.id}`, {
          radius: 8.2,
          tessellation: 30,
        }, this.scene);
        inner.rotation.x = Math.PI / 2;
        inner.position.y = 0.22;
        inner.isPickable = false;
        const innerMat = new B.StandardMaterial(`cp-inner-mat-${pad.id}`, this.scene);
        innerMat.diffuseColor = B.Color3.Black();
        innerMat.emissiveColor = new B.Color3(0.18, 0.26, 0.34);
        innerMat.disableLighting = true;
        innerMat.alpha = 0.16;
        inner.material = innerMat;

        // pad.color is stable for the pad's lifetime — parse it once here
        // instead of every 10Hz tick, with the +0.08 owner boost pre-applied.
        const ownerColor = parseColor(pad.color || '#7ef7ff');
        const ownerCapture = new B.Color3(
          Math.min(1, ownerColor.r + 0.08),
          Math.min(1, ownerColor.g + 0.08),
          Math.min(1, ownerColor.b + 0.08)
        );
        entry = { base, baseMat, ring, ringMat, inner, innerMat, ownerCapture };
        this.capturePads.set(pad.id, entry);
      }

      const pos = pad.position;
      const x = pos[0], z = pos[1];
      const ready = pad.is_ready !== false;
      const progress = Math.max(0, Math.min(1, (pad.progress_ticks || 0) / Math.max(1, pad.capture_ticks || 1)));
      const contested = !!pad.is_contested;
      const contenderCount = pad.contender_count || 0;
      const neutral = this._capturePadNeutral;
      const locked = this._capturePadLocked;
      const contestedColor = this._capturePadContested;
      const targetColor = ready ? neutral : locked;

      entry.base.position.set(x, 0.7, z);
      entry.inner.position.set(x, 0.22, z);

      let captureColor = targetColor;
      if (pad.owner_id || pad.capturing_bot_id) {
        captureColor = entry.ownerCapture;
      }
      if (contested) {
        captureColor = contestedColor;
      }

      // Scratch Color3s keep the per-tick lerp/scale math allocation-free.
      const emissive = this._scratchA;
      B.Color3.LerpToRef(targetColor, captureColor, Math.max(progress, pad.owner_id ? 1 : 0), emissive);
      emissive.scaleToRef(0.72, this._scratchB);
      entry.baseMat.emissiveColor.copyFrom(this._scratchB);
      entry.ringMat.emissiveColor.copyFrom(emissive);
      emissive.scaleToRef(0.55, this._scratchB);
      entry.innerMat.emissiveColor.copyFrom(this._scratchB);

      entry.baseMat.alpha = ready ? 0.34 + 0.08 * progress : 0.22;
      entry.ringMat.alpha = ready ? 0.48 + 0.22 * progress : 0.2;
      entry.innerMat.alpha = ready ? 0.10 + 0.18 * progress : 0.06;

      entry.inner.scaling.set(0.84 + progress * 0.38, 0.84 + progress * 0.38, 1);
      if (contested) {
        entry.baseMat.alpha = Math.max(entry.baseMat.alpha, 0.32);
        entry.ringMat.alpha = Math.max(entry.ringMat.alpha, 0.66);
      }

      // Ambient bob/spin/pulse runs per-frame in _animateAmbient.
      entry.x = x;
      entry.z = z;
      entry.ready = ready;
      entry.progress = progress;
      entry.contested = contested;
      entry.contenderCount = contenderCount;
    }

    for (const [id, entry] of this.capturePads) {
      if (!seen.has(id)) {
        entry.base.dispose();
        entry.baseMat.dispose();
        entry.ring.dispose();
        entry.ringMat.dispose();
        entry.inner.dispose();
        entry.innerMat.dispose();
        this.capturePads.delete(id);
      }
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // HAZARD ZONES — electric floor panels with spark particles
  // ═══════════════════════════════════════════════════════════════════
  _updateStaffImpacts(impacts) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const impact of impacts) {
      const pos = impact.position || [0, 0];
      const id = `${impact.owner_id || 'staff'}-${pos[0]}-${pos[1]}`;
      seen.add(id);
      let entry = this.staffImpacts.get(id);
      if (!entry) {
        const outer = B.MeshBuilder.CreateTorus(`staff-ring-${id}`, {
          diameter: Math.max(18, (impact.radius || 1) * 40),
          thickness: 1.6,
          tessellation: 48,
        }, this.scene);
        outer.rotation.x = Math.PI / 2;
        outer.isPickable = false;
        const outerMat = new B.StandardMaterial(`staff-ring-mat-${id}`, this.scene);
        outerMat.diffuseColor = B.Color3.Black();
        outerMat.emissiveColor = new B.Color3(0.7, 0.3, 1.0);
        outerMat.disableLighting = true;
        outer.material = outerMat;

        const disc = B.MeshBuilder.CreateDisc(`staff-disc-${id}`, {
          radius: Math.max(10, (impact.radius || 1) * 20),
          tessellation: 40,
        }, this.scene);
        disc.rotation.x = Math.PI / 2;
        disc.position.y = 0.2;
        disc.isPickable = false;
        const discMat = new B.StandardMaterial(`staff-disc-mat-${id}`, this.scene);
        discMat.diffuseColor = B.Color3.Black();
        discMat.emissiveColor = new B.Color3(0.45, 0.15, 0.8);
        discMat.disableLighting = true;
        discMat.alpha = 0.16;
        disc.material = discMat;

        entry = {
          outer,
          outerMat,
          disc,
          discMat,
          initialTicks: Math.max(impact.ticks_left || 1, 1),
        };
        this.staffImpacts.set(id, entry);
        if (this.onStaffImpactCreated) {
          this.onStaffImpactCreated({
            id,
            ownerId: impact.owner_id || null,
            position: [pos[0], pos[1]],
            radius: impact.radius || 1,
            ticksLeft: Math.max(impact.ticks_left || 1, 1),
          });
        }
      }

      entry.initialTicks = Math.max(entry.initialTicks || 1, impact.ticks_left || 1);
      const ticksLeft = Math.max(impact.ticks_left || 1, 1);
      const progress = 1 - Math.min(1, ticksLeft / Math.max(entry.initialTicks, 1));
      const urgency = Math.min(1, 1 / ticksLeft);
      const reveal = Math.max(0, Math.min(1, (progress - 0.18) / 0.82));
      entry.reveal = reveal; // outer-ring pulse applied per-frame in _animateAmbient
      entry.outer.position.set(pos[0], 1.2, pos[1]);
      entry.disc.position.set(pos[0], 0.2, pos[1]);
      // reveal/urgency-driven scale + emissive shift is the visual "staffImpactRings"
      // effect; reveal itself keeps being computed above (read by _animateAmbient's
      // pulse and by the disabled-branch below) regardless of the toggle.
      if (isEnabled('gameplayZoneIndicators', 'staffImpactRings')) {
        entry.disc.scaling.set(0.9 + reveal * 0.18, 0.9 + reveal * 0.18, 1);
        entry.outerMat.emissiveColor.set(0.24 + reveal * 0.52, 0.08 + reveal * 0.28, 0.46 + urgency * 0.5);
        entry.discMat.emissiveColor.set(0.14 + reveal * 0.32, 0.05 + reveal * 0.14, 0.30 + urgency * 0.45);
        entry.outerMat.alpha = 0.04 + reveal * 0.28 + urgency * 0.18;
        entry.discMat.alpha = 0.015 + reveal * 0.11 + urgency * 0.10;
      } else {
        // Static base appearance: reveal=0, urgency=0 equivalent (the ring's
        // resting look right after it spawns), no countdown-driven scale/color/alpha.
        // Also pins entry.outer.scaling here (normally driven by _animateAmbient's
        // per-frame pulse, which is skipped entirely while this toggle is off).
        entry.disc.scaling.set(0.9, 0.9, 1);
        entry.outer.scaling.set(0.88, 0.88, 1);
        entry.outerMat.emissiveColor.set(0.24, 0.08, 0.46);
        entry.discMat.emissiveColor.set(0.14, 0.05, 0.30);
        entry.outerMat.alpha = 0.04;
        entry.discMat.alpha = 0.015;
      }
    }

    for (const [id, entry] of this.staffImpacts) {
      if (!seen.has(id)) {
        entry.outer.dispose();
        entry.disc.dispose();
        entry.outerMat.dispose();
        entry.discMat.dispose();
        this.staffImpacts.delete(id);
      }
    }
  }

  _updateBurnFields(fields) {
    const B = window.BABYLON;
    const seen = new Set();

    for (const field of fields) {
      seen.add(field.id);
      let entry = this.burnFields.get(field.id);
      if (!entry) {
        const radius = Math.max(8, (field.radius || 1) * 20);
        const disc = B.MeshBuilder.CreateDisc(`burn-disc-${field.id}`, {
          radius,
          tessellation: 34,
        }, this.scene);
        disc.rotation.x = Math.PI / 2;
        disc.position.y = 0.18;
        disc.isPickable = false;
        const discMat = new B.StandardMaterial(`burn-disc-mat-${field.id}`, this.scene);
        discMat.diffuseColor = B.Color3.Black();
        discMat.emissiveColor = new B.Color3(1.0, 0.46, 0.16);
        discMat.disableLighting = true;
        discMat.alpha = 0.18;
        disc.material = discMat;

        const ring = B.MeshBuilder.CreateTorus(`burn-ring-${field.id}`, {
          diameter: radius * 2.05,
          thickness: Math.max(1.2, radius * 0.08),
          tessellation: 32,
        }, this.scene);
        ring.rotation.x = Math.PI / 2;
        ring.position.y = 0.75;
        ring.isPickable = false;
        const ringMat = new B.StandardMaterial(`burn-ring-mat-${field.id}`, this.scene);
        ringMat.diffuseColor = B.Color3.Black();
        ringMat.emissiveColor = new B.Color3(1.0, 0.58, 0.22);
        ringMat.disableLighting = true;
        ringMat.alpha = 0.42;
        ring.material = ringMat;

        entry = { disc, discMat, ring, ringMat, baseRadius: radius };
        this.burnFields.set(field.id, entry);
      }

      const pos = field.position || [0, 0];
      const ticksLeft = Math.max(field.ticks_left || 1, 1);
      const life = Math.min(1, ticksLeft / 12);
      entry.disc.position.set(pos[0], 0.18, pos[1]);
      entry.discMat.alpha = 0.08 + life * 0.14;
      entry.ringMat.alpha = 0.14 + life * 0.28;
      // Pulse/bob applied per-frame in _animateAmbient.
      entry.x = pos[0];
      entry.z = pos[1];
    }

    for (const [id, entry] of this.burnFields) {
      if (!seen.has(id)) {
        entry.disc.dispose();
        entry.ring.dispose();
        entry.discMat.dispose();
        entry.ringMat.dispose();
        this.burnFields.delete(id);
      }
    }
  }

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

      entry.active = !!zone.active;
      if (zone.active) {
        // Electrical zapping on; pulse applied per-frame in _animateAmbient.
        entry.zaps.emitRate = 40;
        entry.sparks.emitRate = 15;
      } else {
        // Dim outline, no electricity
        entry.edgeMat.emissiveColor.set(0.3, 0.05, 0.02);
        entry.edgeMat.alpha = 0.25;
        entry.zaps.emitRate = 0;
        entry.sparks.emitRate = 0;
      }
      // Effect toggle overrides whatever emitRate the active/inactive branch
      // above just set: particles off regardless of zone state, without
      // touching the active-state logic (outline color/alpha, pulse) itself.
      if (!isEnabled('gameplayZoneIndicators', 'hazardZoneEffects')) {
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
        // false: zaps/sparks share _glowTex — see note in _updateTeleportPads.
        entry.zaps.dispose(false);
        entry.sparks.dispose(false);
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
      // Blink applied per-frame in _animateAmbient.
      mesh.metadata = { armed: !!mine.armed };
      if (!mine.armed) {
        mesh.material.emissiveColor.set(0.1, 0.1, 0.05);
      }
    }

    for (const [id, mesh] of this.landmines) {
      if (!seen.has(id)) {
        // Each mine owns a per-id StandardMaterial; mesh.dispose() alone
        // orphans it (measured: 71 mine-mats accumulated in minutes).
        if (mesh.material) mesh.material.dispose();
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
      entry.inner.position.set(x, 5, z);
      entry.vortex.emitter.x = x;
      entry.vortex.emitter.z = z;
      // Spin/sway/pulse applied per-frame in _animateAmbient.
      entry.x = x;
      entry.z = z;
    }

    for (const [id, entry] of this.gravityWells) {
      if (!seen.has(id)) {
        entry.ring.dispose();
        entry.inner.dispose();
        // false: vortex shares _glowTex — see note in _updateTeleportPads.
        entry.vortex.dispose(false);
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

    // Remove tiles the server stopped sending (round ended, sudden death
    // reset) — without this, last round's void planes and their per-tile
    // materials leak and linger on the next round's floor.
    for (const [key, mesh] of this.voidTiles) {
      if (seen.has(key)) continue;
      if (mesh.material) mesh.material.dispose();
      mesh.dispose();
      this.voidTiles.delete(key);
    }
  }

  // ═══════════════════════════════════════════════════════════════════
  // BOUNTY — golden spinning crown ring above the target
  // ═══════════════════════════════════════════════════════════════════
  _updateBounty() {
    const B = window.BABYLON;
    const streakLeader = (this.bountyBots || [])
      .filter((b) => b && b.is_alive && Number(b.kill_streak || 0) >= 2)
      .sort((a, b) => {
        const streakGap = Number(b.kill_streak || 0) - Number(a.kill_streak || 0);
        if (streakGap !== 0) return streakGap;
        const killGap = Number(b.round_kills || 0) - Number(a.round_kills || 0);
        if (killGap !== 0) return killGap;
        // Final stable tiebreak by id: the input order is only incidentally
        // stable (server name-sorts), so make the leader deterministic here.
        return String(a.bot_id || a.id || '').localeCompare(String(b.bot_id || b.id || ''));
      })[0];
    const highlightId = this.bountyTargetId || streakLeader?.bot_id || streakLeader?.id || null;
    if (!highlightId) {
      if (this.bountyGroup) {
        this.bountyGroup.ring.visibility = 0;
        if (this.bountyGroup.sparkle) this.bountyGroup.sparkle.emitRate = 0;
      }
      return;
    }

    this.bountyTargetId = highlightId;
    const target = this.bountyBots.find(b => b.bot_id === highlightId || b.id === highlightId);
    if (!target || !target.is_alive) {
      // Clear the latch so a dead holder does not stick forever: in heuristic
      // mode (empty server target) highlightId short-circuits on
      // this.bountyTargetId, so without this the crown would never reappear
      // for a new streak leader after the holder dies. Safe in server mode too:
      // a live server target is re-sent every update and re-adopted at once.
      this.bountyTargetId = null;
      if (this.bountyGroup) {
        this.bountyGroup.ring.visibility = 0;
        if (this.bountyGroup.sparkle) this.bountyGroup.sparkle.emitRate = 0;
      }
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
      // Gold sparkle drip under the crown so the streak leader - the
      // spectator's "who is winning" anchor - reads from across the room.
      // Shares _glowTex (dispose(false)); emitter tracked in animate().
      const sparkle = new B.ParticleSystem('bountySparkle', 30, this.scene);
      sparkle.particleTexture = this._glowTex;
      sparkle.emitter = new B.Vector3(0, 23, 0);
      sparkle.createPointEmitter(new B.Vector3(-0.4, -0.2, -0.4), new B.Vector3(0.4, 0.2, 0.4));
      sparkle.color1 = new B.Color4(1, 0.85, 0.2, 0.8);
      sparkle.color2 = new B.Color4(1, 0.95, 0.6, 0.6);
      sparkle.colorDead = new B.Color4(0.6, 0.45, 0.05, 0);
      sparkle.minSize = 2; sparkle.maxSize = 5;
      sparkle.minLifeTime = 0.4; sparkle.maxLifeTime = 0.9;
      sparkle.minEmitPower = 1; sparkle.maxEmitPower = 4;
      sparkle.gravity = new B.Vector3(0, -6, 0);
      sparkle.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      sparkle.emitRate = 0;
      sparkle.start();
      this.bountyGroup = { ring, sparkle, curX: 0, curZ: 0, initialized: false };
    }
  }

  animate(botEntries, dt) {
    // Advance the ambient clock per-frame (dt-based, 10 units/sec to keep
    // the same phase speeds the old 10Hz tick counter had) and run all idle
    // pulses/bobs/spins so they're smooth instead of stepping at 10Hz.
    const d = Math.min(dt || 0.016, 0.1);
    this._tick += d * 10;
    this._animateAmbient(d);

    if (!this.bountyGroup || !this.bountyTargetId) return;
    if (!isEnabled('objectiveIndicators', 'bountyCrown')) {
      // Hide the ring and stop the sparkle drip, but keep bountyTargetId/
      // bountyGroup tracking alive in _updateBounty() so re-enabling snaps
      // back to the correct target instantly instead of waiting a beat.
      this.bountyGroup.ring.visibility = 0;
      if (this.bountyGroup.sparkle) this.bountyGroup.sparkle.emitRate = 0;
      return;
    }
    const targetEntry = botEntries && botEntries.get ? botEntries.get(this.bountyTargetId) : null;
    const fallback = this.bountyBots.find((b) => b.bot_id === this.bountyTargetId || b.id === this.bountyTargetId);
    const sourcePos = targetEntry?.root?.position
      ? [targetEntry.root.position.x, targetEntry.root.position.z]
      : fallback?.position;
    if (!sourcePos) {
      this.bountyGroup.ring.visibility = 0;
      if (this.bountyGroup.sparkle) this.bountyGroup.sparkle.emitRate = 0;
      return;
    }

    const tx = sourcePos[0];
    const tz = sourcePos[1];
    const g = this.bountyGroup;
    // usingEntry: true when we have the bot's already-smoothed render position
    // (bots.js lerps it toward server snapshots at rate 6). In that case the
    // ring follows it DIRECTLY - no second exponential chase - so the ~270ms
    // compounded lag that made the ring drag behind moving bots is gone.
    const usingEntry = !!(targetEntry && targetEntry.root && targetEntry.root.position);
    const TRANS_DUR = 0.35; // seconds, target-switch ease
    if (!g.initialized) {
      g.curX = tx;
      g.curZ = tz;
      g.initialized = true;
      g.followId = this.bountyTargetId;
      g.transT = TRANS_DUR; // no ease on first appearance
    }
    // Target switch: ease from the ring's current position to the new target
    // over a fixed duration (cubic-out) instead of an endless cross-arena
    // glide that oscillated when the server flapped the target.
    if (g.followId !== this.bountyTargetId) {
      g.followId = this.bountyTargetId;
      g.transFromX = g.curX;
      g.transFromZ = g.curZ;
      g.transT = 0;
    }
    if (g.transT < TRANS_DUR) {
      g.transT = Math.min(TRANS_DUR, g.transT + d);
      const p = g.transT / TRANS_DUR;
      const ease = 1 - Math.pow(1 - p, 3);
      g.curX = g.transFromX + (tx - g.transFromX) * ease;
      g.curZ = g.transFromZ + (tz - g.transFromZ) * ease;
    } else if (usingEntry) {
      g.curX = tx;
      g.curZ = tz;
    } else {
      // Raw fallback server position (entry not yet created): steps at 10Hz,
      // so smooth it lightly. Uses real dt (d), not the old Math.max floor
      // that inflated the rate on high-refresh displays.
      const lerp = 1 - Math.exp(-12 * d);
      g.curX += (tx - g.curX) * lerp;
      g.curZ += (tz - g.curZ) * lerp;
    }
    g.ring.position.set(g.curX, 25 + Math.sin(this._tick * 0.06) * 3, g.curZ);
    g.ring.rotation.y += 0.4 * d;
    g.ring.visibility = 1;
    if (g.sparkle) {
      // Track the crown; drip toward the bot (emitter mutated in place).
      g.sparkle.emitter.x = g.curX;
      g.sparkle.emitter.y = 23;
      g.sparkle.emitter.z = g.curZ;
      g.sparkle.emitRate = 18;
    }
  }

  /** @private Per-frame idle animations for gameplay entities. All phase
   * math uses this._tick (advanced dt-based at 10 units/sec) so speeds are
   * identical to the old 10Hz stepping, just smooth. */
  _animateAmbient(dt) {
    const t = this._tick;

    for (const [, e] of this.teleportPads) {
      if (e.x === undefined) continue;
      e.ring.position.set(e.x, 2 + Math.sin(t * 0.04) * (e.ready ? 0.5 : 0.15), e.z);
      e.ring.rotation.y += (e.ready ? 0.2 : 0.01) * dt;
      e.platform.material.alpha = e.ready
        ? 0.4 + 0.15 * Math.sin(t * 0.06)
        : 0.08;
      e.haloMat.alpha = e.ready
        ? 0.14 + 0.07 * Math.sin(t * 0.05)
        : 0.01;
      const haloScale = e.ready ? 1.0 + Math.sin(t * 0.04) * 0.04 : 0.94;
      e.halo.scaling.set(haloScale, haloScale, 1);
    }

    for (const [, e] of this.capturePads) {
      if (e.x === undefined) continue;
      e.ring.position.set(e.x, 1.8 + Math.sin(t * 0.035) * 0.18, e.z);
      e.ring.rotation.y += ((e.ready ? 0.16 : 0.04) + (e.contested ? 0.3 : 0)) * dt;
      const pulse = 1 + Math.sin(t * 0.05) * (0.02 + e.progress * 0.03 + e.contenderCount * 0.01);
      e.ring.scaling.set(pulse, pulse, 1);
    }

    if (isEnabled('gameplayZoneIndicators', 'staffImpactRings')) {
      for (const [, e] of this.staffImpacts) {
        if (e.reveal === undefined) continue;
        const scale = 0.88 + e.reveal * 0.22 + Math.sin(t * 0.15) * (0.015 + e.reveal * 0.025);
        e.outer.scaling.set(scale, scale, 1);
      }
    }
    // When disabled, _updateStaffImpacts already pins entry.outer.scaling to
    // its static base value every 10Hz tick, so this per-frame pulse is simply
    // skipped rather than needing its own frozen-scale branch here.

    const burnPulseOn = isEnabled('gameplayZoneIndicators', 'burnFieldPulse');
    for (const [, e] of this.burnFields) {
      if (e.x === undefined) continue;
      if (burnPulseOn) {
        const pulse = 1 + Math.sin(t * 0.16) * 0.06;
        e.ring.position.set(e.x, 0.8 + Math.sin(t * 0.08) * 0.18, e.z);
        e.disc.scaling.set(pulse, pulse, 1);
        e.ring.scaling.set(0.96 + pulse * 0.08, 0.96 + pulse * 0.08, 1);
      } else {
        // Static rest state: mid-pulse Y height, unscaled disc/ring (pulse == 1).
        e.ring.position.set(e.x, 0.8, e.z);
        e.disc.scaling.set(1, 1, 1);
        e.ring.scaling.set(1.04, 1.04, 1);
      }
    }

    for (const mesh of this.landmines.values()) {
      if (mesh.metadata && mesh.metadata.armed) {
        const blink = Math.sin(t * 0.3) > 0 ? 0.9 : 0.2;
        mesh.material.emissiveColor.set(blink, 0.05, 0.05);
      }
    }

    for (const [, e] of this.hazardZones) {
      if (e.active) {
        const pulse = 0.7 + 0.3 * Math.sin(t * 0.2);
        e.edgeMat.emissiveColor.set(1.0, 0.15 * pulse, 0.05);
        e.edgeMat.alpha = pulse;
      }
    }

    const gravitySwirlOn = isEnabled('gameplayZoneIndicators', 'gravityWellSwirl');
    for (const [, e] of this.gravityWells) {
      if (e.x === undefined) continue;
      if (gravitySwirlOn) {
        e.ring.rotation.y += 0.6 * dt;
        e.ring.rotation.x = Math.sin(t * 0.03) * 0.3;
        e.inner.rotation.y -= 1.0 * dt;
        e.ring.material.alpha = 0.4 + 0.3 * Math.sin(t * 0.08);
      } else {
        // Freeze rotation (no increment) rather than removing the mesh;
        // hold alpha at its pulse midpoint for a static look.
        e.ring.material.alpha = 0.4;
      }
    }

    if (this.flags) {
      // Old behavior: 0.25/update at 10Hz — equivalent continuous rate ~2.9/s.
      const lerp = 1 - Math.exp(-3 * dt);
      for (const [, e] of this.flags) {
        if (e.tx === undefined) continue;
        e.curX += (e.tx - e.curX) * lerp;
        e.curZ += (e.tz - e.curZ) * lerp;
        const carried = e.status === 'carried';
        e.pole.position.set(e.curX, carried ? 18 : 13, e.curZ);
        e.bannerMat.alpha = e.status === 'dropped'
          ? 0.5 + 0.4 * Math.abs(Math.sin(t * 0.15))
          : 0.9;
        e.pole.rotation.y = carried ? t * 0.05 : 0;
        // Comet trail while carried: the exp-lerp position above sweeps the
        // emitter smoothly, leaving a genuine team-colored tail across the
        // arena. Emitter Vector3 mutated in place (allocation-free).
        if (e.trail) {
          e.trail.emitter.x = e.curX;
          e.trail.emitter.y = 14;
          e.trail.emitter.z = e.curZ;
          e.trail.emitRate = (carried && isEnabled('objectiveIndicators', 'flagComet')) ? 28 : 0;
        }
      }
    }
  }

  dispose() {
    for (const [, e] of this.teleportPads) {
      e.platform.dispose(); e.ring.dispose(); e.beam.dispose(false); e.swirl.dispose(false); e.halo.dispose(); e.haloMat.dispose();
    }
    for (const [, e] of this.hazardZones) { e.edges.forEach(m => m.dispose()); e.parent.dispose(); e.zaps.dispose(false); e.sparks.dispose(false); }
    for (const [, e] of this.burnFields) { e.disc.dispose(); e.ring.dispose(); e.discMat.dispose(); e.ringMat.dispose(); }
    for (const m of this.landmines.values()) { if (m.material) m.material.dispose(); m.dispose(); }
    for (const [, e] of this.gravityWells) { e.ring.dispose(); e.inner.dispose(); e.vortex.dispose(false); }
    for (const [, e] of this.staffImpacts) { e.outer.dispose(); e.disc.dispose(); e.outerMat.dispose(); e.discMat.dispose(); }
    for (const m of this.voidTiles.values()) { if (m.material) m.material.dispose(); m.dispose(); }
    if (this.flags) {
      for (const [, e] of this.flags) {
        e.pole.dispose();
        e.base.dispose();
        if (e.trail) e.trail.dispose(false); // shared _glowTex freed below
      }
      this.flags.clear();
    }
    if (this.bountyGroup) {
      this.bountyGroup.ring.dispose();
      if (this.bountyGroup.sparkle) this.bountyGroup.sparkle.dispose(false);
    }
    this.teleportPads.clear();
    this.hazardZones.clear();
    this.burnFields.clear();
    this.landmines.clear();
    this.gravityWells.clear();
    this.staffImpacts.clear();
    this.voidTiles.clear();
    // Shared glow texture is only actually freed here, once, after every
    // particle system that referenced it has been disposed without it.
    if (this._glowTex) {
      this._glowTex.dispose();
      this._glowTex = null;
    }
  }
}
