'use strict';

/**
 * Short, fluid movement wakes. Each bot owns one fine filament and an optional
 * bounded particle emitter; no floor-wide sheets or persistent path decals.
 * @module renderer/trails
 */

import { isEnabled } from '../settings.js';

const MAX_HISTORY = 24;
const SAMPLE_INTERVAL = 0.1;
const MAX_RENDERED_TRAILS = 48;
const RENDER_QUEUE_REBUILD_INTERVAL_MS = 250;
const MAX_PARTICLE_SYSTEMS = 24;
const MAX_PARTICLES_PER_TRAIL = 28;
const TRAIL_SELECTION_HYSTERESIS_SQ = 40 * 40;
const TRAIL_Y = 0.65;
const MAX_WAKE_LENGTH = 38;
const MAX_WAKE_AGE = 0.9;
const GEOMETRY_INTERVAL = 1 / 30;

function particleStyle(emitRate, gravityY, options = {}) {
  return Object.freeze({
    emitRate, gravityY,
    minSize: options.minSize ?? 0.3,
    maxSize: options.maxSize ?? 0.9,
    minLife: options.minLife ?? 0.22,
    maxLife: options.maxLife ?? 0.65,
    spread: options.spread ?? 1.1,
    rise: options.rise ?? 1.4,
    drag: options.drag ?? 1.8,
    swirl: options.swirl ?? 0,
    stretch: options.stretch ?? 1,
    soft: options.soft === true,
    motion: options.motion ?? 'drift',
  });
}

function trailStyle(key, primary, secondary, width, alpha, particles, flow = {}) {
  return Object.freeze({
    key, primary, secondary, width, alpha, particles,
    // Width is a relative style weight. Actual filaments are sub-unit strokes.
    curl: flow.curl ?? 0.24,
    frequency: flow.frequency ?? 2.4,
    lift: flow.lift ?? 0.35,
    filament: flow.filament ?? 0.3,
  });
}

const STANDARD_STYLE = trailStyle('standard', '#72bccb', '#c5e8ed', 0.7, 0.22, null,
  {curl: 0.08, lift: 0.05, filament: 0.18});

// Stable catalog IDs; every signature is authored locally, without remote assets.
// Hot sparks decelerate and cool; dust expands; droplets arc; plasma curls.
const TRAIL_STYLES = Object.freeze({
  ember_sparks: trailStyle('ember_sparks', '#e65021', '#ffd8a2', 1.3, 0.65,
    particleStyle(30, 1.8, {motion: 'ember', rise: 2.8, drag: 2.3, stretch: 2.2}), {curl: 0.14, lift: 0.65}),
  frost_shards: trailStyle('frost_shards', '#8ac4dd', '#e7fbff', 1.2, 0.6,
    particleStyle(25, -4.2, {motion: 'fall', rise: 2.3, stretch: 2.6, minSize: 0.18, maxSize: 0.55}), {curl: 0.1, filament: 0.2}),
  ion_stream: trailStyle('ion_stream', '#348ac4', '#9ceff2', 1.25, 0.66,
    particleStyle(36, 0.2, {motion: 'jet', spread: 0.24, rise: 0.2, stretch: 3.8, drag: 0.8}), {curl: 0.12, frequency: 4, filament: 0.26}),
  plasma_ribbon: trailStyle('plasma_ribbon', '#8159c9', '#ef9fd7', 1.45, 0.64,
    particleStyle(30, 0.7, {motion: 'vortex', swirl: 2.2, spread: 0.7, soft: true, maxSize: 1.2}), {curl: 0.65, frequency: 3.2, lift: 0.7}),
  void_motes: trailStyle('void_motes', '#655190', '#b6a0de', 1.2, 0.58,
    particleStyle(24, 0.3, {motion: 'orbit', swirl: -1.4, rise: 0.3, drag: 2.6, minLife: 0.55, maxLife: 1.05}), {curl: 0.48, frequency: 1.6, filament: 0.17}),
  solar_wake: trailStyle('solar_wake', '#e59337', '#fff0c1', 1.5, 0.68,
    particleStyle(32, 1.3, {motion: 'corona', rise: 2.5, swirl: 0.8, soft: true, maxSize: 1.3}), {curl: 0.38, lift: 0.8, filament: 0.4}),
  lunar_dust: trailStyle('lunar_dust', '#94a2b6', '#e0e7f1', 1.25, 0.58,
    particleStyle(24, -0.5, {motion: 'dust', spread: 1.8, rise: 0.65, soft: true, drag: 3, minLife: 0.6, maxLife: 1.15}), {curl: 0.2, lift: 0.15, filament: 0.15}),
  comet_tail: trailStyle('comet_tail', '#578fae', '#d8f7ff', 1.4, 0.7,
    particleStyle(38, -0.2, {motion: 'jet', spread: 0.45, rise: 0.4, drag: 0.6, stretch: 4.5, minLife: 0.3, maxLife: 0.8}), {curl: 0.08, filament: 0.4}),
  nebula_pulse: trailStyle('nebula_pulse', '#836baf', '#dcaec9', 1.4, 0.6,
    particleStyle(26, 0.4, {motion: 'vortex', swirl: 1, rise: 0.7, soft: true, minSize: 0.6, maxSize: 1.5}), {curl: 0.7, frequency: 1.8, lift: 0.9, filament: 0.24}),
  storm_arcs: trailStyle('storm_arcs', '#6587c4', '#dcf5ff', 1.25, 0.72,
    particleStyle(34, -0.5, {motion: 'arc', spread: 1.8, rise: 1, minLife: 0.12, maxLife: 0.3, stretch: 3.2}), {curl: 0.28, frequency: 8, filament: 0.18}),
  static_glitch: trailStyle('static_glitch', '#51b49f', '#d798c6', 1.2, 0.64,
    particleStyle(28, 0, {motion: 'step', spread: 1.4, rise: 0.2, minLife: 0.15, maxLife: 0.35, stretch: 2.1}), {curl: 0.32, frequency: 10, filament: 0.15}),
  pixel_scatter: trailStyle('pixel_scatter', '#76b886', '#a9c8df', 1.2, 0.6,
    particleStyle(26, -3, {motion: 'step', spread: 2, rise: 2.6, drag: 1.2, minSize: 0.24, maxSize: 0.56}), {curl: 0.18, frequency: 6, filament: 0.17}),
  data_stream: trailStyle('data_stream', '#43a391', '#b0f1de', 1.2, 0.67,
    particleStyle(32, 0, {motion: 'jet', spread: 0.18, rise: 0, stretch: 3, minSize: 0.18, maxSize: 0.45}), {curl: 0.04, frequency: 5, filament: 0.22}),
  holo_prism: trailStyle('holo_prism', '#76b8c7', '#d5a5ce', 1.35, 0.63,
    particleStyle(27, 0.1, {motion: 'orbit', swirl: 2.7, rise: 0.5, stretch: 1.6, minSize: 0.32, maxSize: 0.8}), {curl: 0.5, frequency: 4.2, lift: 0.55}),
  toxic_spores: trailStyle('toxic_spores', '#859e39', '#d7e6a0', 1.3, 0.61,
    particleStyle(25, 0.8, {motion: 'spore', spread: 1.9, rise: 0.7, swirl: 0.8, soft: true, minLife: 0.6, maxLife: 1.2}), {curl: 0.4, frequency: 1.4, lift: 0.6, filament: 0.17}),
  verdant_leaves: trailStyle('verdant_leaves', '#438e60', '#c4d9a0', 1.25, 0.59,
    particleStyle(24, -2.5, {motion: 'flutter', spread: 1.7, rise: 2, swirl: 1.9, stretch: 1.8, minLife: 0.5, maxLife: 1}), {curl: 0.25, frequency: 2, filament: 0.17}),
  sand_wake: trailStyle('sand_wake', '#a78c61', '#e3d3ac', 1.4, 0.59,
    particleStyle(32, -2.8, {motion: 'dust', spread: 2.6, rise: 1.6, drag: 3.4, soft: true, minSize: 0.4, maxSize: 1.3}), {curl: 0.12, lift: 0.08, filament: 0.17}),
  magma_cinders: trailStyle('magma_cinders', '#c44424', '#efb564', 1.35, 0.67,
    particleStyle(29, 3.2, {motion: 'ember', spread: 1.2, rise: 1.3, drag: 1.2, minLife: 0.4, maxLife: 0.9}), {curl: 0.32, lift: 0.8, filament: 0.35}),
  ocean_spray: trailStyle('ocean_spray', '#4f9cb5', '#d0eeef', 1.35, 0.62,
    particleStyle(31, -6, {motion: 'droplet', spread: 2, rise: 3.7, stretch: 1.6, drag: 0.35}), {curl: 0.32, frequency: 2.5, lift: 0.15}),
  gilded_dust: trailStyle('gilded_dust', '#be9a4c', '#f9e8b0', 1.25, 0.66,
    particleStyle(26, -0.9, {motion: 'glint', spread: 1.5, rise: 1, drag: 2.6, minSize: 0.18, maxSize: 0.6, minLife: 0.5, maxLife: 1}), {curl: 0.15, filament: 0.21}),
  rune_sparks: trailStyle('rune_sparks', '#8c77b5', '#9be3df', 1.25, 0.66,
    particleStyle(25, 0.6, {motion: 'orbit', swirl: -3.2, rise: 1.1, stretch: 2, minLife: 0.3, maxLife: 0.7}), {curl: 0.45, frequency: 5, lift: 0.65}),
  phantom_smoke: trailStyle('phantom_smoke', '#747787', '#bbc3cf', 1.45, 0.58,
    particleStyle(24, 0.5, {motion: 'smoke', spread: 1.4, rise: 1.1, swirl: 0.6, drag: 3, soft: true, minSize: 0.7, maxSize: 1.8, minLife: 0.6, maxLife: 1.25}), {curl: 0.65, frequency: 1.3, lift: 1, filament: 0.12}),
  gear_sparks: trailStyle('gear_sparks', '#b97e46', '#f3dcaa', 1.2, 0.66,
    particleStyle(35, -5.6, {motion: 'spark', spread: 2.2, rise: 3.1, drag: 0.7, stretch: 3.4, minLife: 0.16, maxLife: 0.45}), {curl: 0.1, frequency: 6, filament: 0.19}),
  bounty_flare: trailStyle('bounty_flare', '#dba044', '#ffe3ba', 1.5, 0.72,
    particleStyle(34, 1.6, {motion: 'corona', spread: 1.3, rise: 2, swirl: -0.8, stretch: 1.8}), {curl: 0.35, frequency: 3.5, lift: 0.75, filament: 0.4}),
});

/** Resolve an untrusted cosmetic key to one local, bounded style. */
export function resolveTrailStyle(assetKey) {
  if (typeof assetKey !== 'string') return STANDARD_STYLE;
  const key = assetKey.trim().toLowerCase();
  return Object.hasOwn(TRAIL_STYLES, key) ? TRAIL_STYLES[key] : STANDARD_STYLE;
}

function parseColor(B, value, fallback) {
  try {
    return B.Color3.FromHexString(value);
  } catch {
    return B.Color3.FromHexString(fallback);
  }
}

function cosmeticTrailKey(entry) {
  const raw = entry?.botData?.cosmetics?.trail;
  return typeof raw === 'string' && raw.trim() ? raw.trim().toLowerCase() : 'standard';
}

function pageHidden() {
  return typeof document !== 'undefined' && document.hidden === true;
}

function entryPosition(entry) {
  if (entry?.root?.parent && typeof entry.root.getAbsolutePosition === 'function') {
    return entry.root.getAbsolutePosition();
  }
  return entry?.root?.position;
}

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene, options = {}) {
    this.scene = scene;
    this.options = options;
    /** @type {Map<string, Object>} */
    this.trails = new Map();
    this.particleSystemCount = 0;
    this.sharedRibbonMaterial = null;
    this.sharedParticleTexture = null;
    this._styleColors = new Map();
    this._renderQueue = [];
    this._paidCandidates = [];
    this._standardCandidates = [];
    this._queueRefreshAt = 0;
    this._queueEntriesSize = -1;
    this._motionQuery = typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null;
    this._time = 0;
  }

  _enabled() {
    return this.options.forceEnabled === true || isEnabled('movementTrails', 'botTrails');
  }

  _reducedMotion() {
    return this.options.reducedMotion === true || this._motionQuery?.matches === true;
  }

  _getSharedRibbonMaterial() {
    if (this.sharedRibbonMaterial) return this.sharedRibbonMaterial;
    const B = window.BABYLON;
    const material = new B.StandardMaterial('arena-cosmetic-trails-shared', this.scene);
    material.emissiveColor = new B.Color3(0.8, 0.8, 0.8);
    material.diffuseColor = new B.Color3(1, 1, 1);
    material.disableLighting = true;
    material.backFaceCulling = false;
    material.alpha = 1;
    material.useVertexAlpha = true;
    // Additive blending makes every wake read as light on the near-black
    // arena floor instead of a translucent decal.
    material.alphaMode = B.Engine?.ALPHA_ADD ?? 1;
    material.disableDepthWrite = true;
    material.freeze();
    this.sharedRibbonMaterial = material;
    return material;
  }

  _getSharedParticleTexture() {
    if (this.sharedParticleTexture) return this.sharedParticleTexture;
    const B = window.BABYLON;
    const texture = new B.DynamicTexture('arena-cosmetic-trail-particle', 16, this.scene, false);
    texture.hasAlpha = true;
    const context = texture.getContext();
    context.clearRect(0, 0, 16, 16);
    const gradient = context.createRadialGradient(8, 8, 0, 8, 8, 8);
    gradient.addColorStop(0, 'rgba(255,255,255,1)');
    gradient.addColorStop(0.16, 'rgba(255,255,255,0.72)');
    gradient.addColorStop(0.55, 'rgba(255,255,255,0.18)');
    gradient.addColorStop(1, 'rgba(255,255,255,0)');
    context.fillStyle = gradient;
    context.fillRect(0, 0, 16, 16);
    texture.update(false);
    this.sharedParticleTexture = texture;
    return texture;
  }

  _getStyleColors(style) {
    let colors = this._styleColors.get(style.key);
    if (colors) return colors;
    const B = window.BABYLON;
    const primary = parseColor(B, style.primary, STANDARD_STYLE.primary);
    const secondary = parseColor(B, style.secondary, STANDARD_STYLE.secondary);
    colors = Object.freeze({primary, secondary});
    this._styleColors.set(style.key, colors);
    return colors;
  }

  _createParticleSystem(botId, entry, style) {
    if (!style.particles || this.particleSystemCount >= MAX_PARTICLE_SYSTEMS) return null;
    const B = window.BABYLON;
    const particles = new B.ParticleSystem(
      `cosmetic-trail-particles-${botId}`,
      MAX_PARTICLES_PER_TRAIL,
      this.scene,
    );
    particles.particleTexture = this._getSharedParticleTexture();
    particles.disposeOnStop = false;
    particles.emitter = entry.root;
    const config = style.particles;
    particles.minEmitBox = new B.Vector3(-0.7, 0.65, -0.7);
    particles.maxEmitBox = new B.Vector3(0.7, 1.4, 0.7);
    particles.direction1 = new B.Vector3(-config.spread, config.rise * 0.6, -config.spread);
    particles.direction2 = new B.Vector3(config.spread, config.rise, config.spread);
    particles.gravity = new B.Vector3(0, config.gravityY, 0);
    particles.minSize = config.minSize;
    particles.maxSize = config.maxSize;
    particles.minLifeTime = config.minLife;
    particles.maxLifeTime = config.maxLife;
    particles.minScaleX = 0.7;
    particles.maxScaleX = 1;
    particles.minScaleY = config.stretch;
    particles.maxScaleY = config.stretch * 1.25;
    particles.emitRate = 0;
    particles.minEmitPower = 1;
    particles.maxEmitPower = 1.6;
    particles.minAngularSpeed = config.soft ? -0.3 : -1.4;
    particles.maxAngularSpeed = config.soft ? 0.3 : 1.4;
    particles.blendMode = config.soft
      ? B.ParticleSystem.BLENDMODE_STANDARD
      : B.ParticleSystem.BLENDMODE_ADD;
    // Babylon multiplies updateSpeed by its 60-Hz animation ratio.
    particles.updateSpeed = 1 / 60;
    if (typeof particles.addSizeGradient === 'function') {
      const birth = config.soft ? 0.35 : 0.75;
      const end = config.soft ? 2.1 : 0.08;
      // Babylon size gradients are absolute sizes, not min/max multipliers.
      particles.addSizeGradient(0, config.minSize * birth, config.maxSize * birth);
      particles.addSizeGradient(0.2, config.minSize, config.maxSize);
      particles.addSizeGradient(1, config.minSize * end, config.maxSize * end);
    }
    if (typeof particles.addVelocityGradient === 'function') {
      particles.addVelocityGradient(0, 1);
      particles.addVelocityGradient(1, Math.exp(-config.drag));
    }
    // Preserve Babylon's integration/recycling and add a smooth force field.
    // This keeps pooling, lifetime, color and size gradients engine-owned.
    const integrate = particles.updateFunction;
    if (typeof integrate === 'function') {
      particles.updateFunction = active => {
        integrate.call(particles, active);
        const step = Math.min(0.05, particles.updateSpeed * (this.scene.getAnimationRatio?.() ?? 1));
        for (const particle of active) {
          const age = particle.age / particle.lifeTime;
          const phase = particle.position.x * 0.41 + particle.position.z * 0.29;
          const curl = config.swirl * step;
          const vx = particle.direction.x;
          const vz = particle.direction.z;
          particle.direction.x += -vz * curl;
          particle.direction.z += vx * curl;
          if (config.motion === 'flutter' || config.motion === 'spore' || config.motion === 'smoke') {
            particle.direction.x += Math.sin(age * 9 + phase) * step * 1.8;
            particle.direction.z += Math.cos(age * 7 + phase) * step * 1.8;
          } else if (config.motion === 'arc' || config.motion === 'step') {
            // Brief little changes in direction, never a screen-wide flash.
            particle.direction.x += Math.sin(age * 28 + phase) * step * 8;
          } else if (config.motion === 'corona') {
            particle.direction.y += Math.sin(age * Math.PI) * step * 2;
          } else if (config.motion === 'glint') {
            particle.color.a = (1 - age) * (0.45 + 0.3 * Math.sin(age * 16 + phase) ** 2);
          }
        }
      };
    }
    this._applyParticleColors(particles, style);
    particles.start();
    this.particleSystemCount += 1;
    return particles;
  }

  _applyParticleColors(particles, style) {
    if (!particles) return;
    const B = window.BABYLON;
    const {primary, secondary} = this._getStyleColors(style);
    particles.color1 = new B.Color4(secondary.r, secondary.g, secondary.b, style.particles.soft ? 0.32 : 0.85);
    particles.color2 = new B.Color4(primary.r, primary.g, primary.b, style.particles.soft ? 0.2 : 0.6);
    particles.colorDead = new B.Color4(primary.r * 0.3, primary.g * 0.3, primary.b * 0.3, 0);
  }

  _disposeParticleSystem(trail) {
    if (!trail?.particles) return;
    trail.particles.stop();
    // The procedural texture belongs to this renderer and is shared by every
    // particle system, so per-bot cleanup must never dispose it.
    trail.particles.dispose(false);
    trail.particles = null;
    this.particleSystemCount = Math.max(0, this.particleSystemCount - 1);
  }

  _hideTrail(trail) {
    if (trail.mesh) trail.mesh.setEnabled(false);
    if (trail.particles) trail.particles.emitRate = 0;
  }

  /** Break wakes on snaps, including suspended-tab and preview resets. */
  reset(botEntries) {
    this._queueRefreshAt = 0;
    for (const [botId, trail] of this.trails) {
      const entry = botEntries?.get(botId);
      const position = entry?._interpReady ? entryPosition(entry) : null;
      trail.history.length = 0;
      trail.timer = 0;
      trail.geometryTimer = 0;
      trail.moving = this.options.staticPreview === true;
      if (position) this._seedHistory(trail.history, position.x, position.z);
      trail.dirty = true;
      this._disposeParticleSystem(trail);
      this._hideTrail(trail);
    }
  }

  _seedHistory(history, x, z) {
    if (this.options.staticPreview === true || this.options.previewPath === true) {
      for (let i = 0; i < MAX_HISTORY; i++) {
        history.push({x: x - 20 * (1 - i / (MAX_HISTORY - 1)), z,
          t: this._time - 0.65 * (1 - i / (MAX_HISTORY - 1))});
      }
    } else history.push({x, z, t: this._time});
  }

  _createTrail(botId, entry, x, z, style) {
    const B = window.BABYLON;
    const left = [];
    const right = [];
    for (let i = 0; i < MAX_HISTORY; i++) {
      left.push(new B.Vector3(x, TRAIL_Y, z));
      right.push(new B.Vector3(x, TRAIL_Y, z));
    }
    const history = [];
    this._seedHistory(history, x, z);
    return {history, mesh: null, particles: null, style, timer: 0,
      geometryTimer: 0, left, right, colors: null, dirty: true,
      moving: this.options.staticPreview === true, entry, botId,
      flowX: 1, flowZ: 0, speed: 0};
  }

  _updateStyle(trail, entry, style) {
    if (trail.style.key === style.key) return;
    this._disposeParticleSystem(trail);
    trail.style = style;
    trail.entry = entry;
    trail.dirty = true;
  }

  /** @private A cached queue is valid only while every queued bot still exists. */
  _queueMembershipValid(botEntries) {
    if (botEntries.size !== this._queueEntriesSize) return false;
    for (const botId of this._renderQueue) {
      if (!botEntries.has(botId)) return false;
    }
    return true;
  }

  _buildRenderQueue(botEntries) {
    // Cadenced rebuild: selection hysteresis keeps membership stable between
    // frames, so re-ranking at display rate bought nothing. Any membership
    // change rebuilds immediately so the seen-set dispose loop below cannot
    // leave a departed bot's ribbon lingering for up to a cadence interval.
    const now = performance.now();
    if (now < this._queueRefreshAt && this._queueMembershipValid(botEntries)) {
      return this._renderQueue;
    }
    this._queueRefreshAt = now + RENDER_QUEUE_REBUILD_INTERVAL_MS;
    this._queueEntriesSize = botEntries.size;

    const queue = this._renderQueue;
    const paid = this._paidCandidates;
    const standard = this._standardCandidates;
    queue.length = 0;
    paid.length = 0;
    standard.length = 0;

    const cameraTarget = this.scene?.activeCamera?.target;
    const hasCameraTarget = Number.isFinite(cameraTarget?.x) && Number.isFinite(cameraTarget?.z);

    // Paid styles are an explicit visual entitlement. Rank them around the
    // current camera target before filling spare capacity with free wakes, so
    // neither free nor off-screen insertion order permanently hides a nearby
    // paid trail. A small existing-trail bonus prevents edge thrash.
    for (const [botId, entry] of botEntries) {
      if (!entry.isAlive || !entry._interpReady) continue;
      const position = entryPosition(entry);
      if (!position) continue;
      const dx = hasCameraTarget ? position.x - cameraTarget.x : 0;
      const dz = hasCameraTarget ? position.z - cameraTarget.z : 0;
      entry._trailPriorityScore = dx * dx + dz * dz
        - (this.trails.has(botId) ? TRAIL_SELECTION_HYSTERESIS_SQ : 0);
      if (resolveTrailStyle(cosmeticTrailKey(entry)).key === 'standard') standard.push(botId);
      else paid.push(botId);
    }

    const byCameraPriority = (leftId, rightId) => {
      const left = botEntries.get(leftId)?._trailPriorityScore ?? 0;
      const right = botEntries.get(rightId)?._trailPriorityScore ?? 0;
      return left - right;
    };
    paid.sort(byCameraPriority);
    standard.sort(byCameraPriority);

    for (let i = 0; i < paid.length && queue.length < MAX_RENDERED_TRAILS; i++) {
      queue.push(paid[i]);
    }
    if (this.options.showStandard !== false) {
      for (let i = 0; i < standard.length && queue.length < MAX_RENDERED_TRAILS; i++) {
        queue.push(standard[i]);
      }
    }
    return queue;
  }

  /** Advance local cosmetic state; gameplay positions are never modified. */
  render(botEntries, dt) {
    if (!botEntries) return;
    const reducedMotion = this._reducedMotion();
    if (!this._enabled() || pageHidden()) {
      for (const [id, trail] of this.trails) {
        this._hideTrail(trail);
        this._disposeParticleSystem(trail);
        if (!botEntries.has(id)) {
          trail.mesh?.dispose();
          this.trails.delete(id);
        }
      }
      this._suspended = true;
      return;
    }
    if (this._suspended) {
      this.reset(botEntries);
      this._suspended = false;
    }
    // Age uses elapsed seconds, while sampling/upload work stays bounded even
    // after a slow frame. Never replay an unbounded queue of missed samples.
    const elapsed = Number.isFinite(dt) ? Math.max(0, dt) : 0;
    this._time += elapsed;
    const seen = new Set();
    for (const botId of this._buildRenderQueue(botEntries)) {
      const entry = botEntries.get(botId);
      const position = entryPosition(entry);
      if (!entry?.isAlive || !entry._interpReady || !Number.isFinite(position?.x)
          || !Number.isFinite(position?.z)) continue;
      seen.add(botId);
      let trail = this.trails.get(botId);
      const raw = entry?.botData?.cosmetics?.trail;
      const style = trail && trail._styleRaw === raw ? trail.style : resolveTrailStyle(raw);
      if (!trail) {
        trail = this._createTrail(botId, entry, position.x, position.z, style);
        this.trails.set(botId, trail);
      } else this._updateStyle(trail, entry, style);
      trail._styleRaw = raw;
      const history = trail.history;
      if (!history.length) this._seedHistory(history, position.x, position.z);
      trail.timer += elapsed;
      trail.geometryTimer += elapsed;
      if (this.options.staticPreview === true) {
        trail.moving = true;
        // Keep this explicitly requested display swatch, with no real motion.
        history.forEach((point, i) => {
          const fraction = 1 - i / Math.max(1, history.length - 1);
          point.x = position.x - 20 * fraction;
          point.z = position.z;
          point.t = this._time - 0.65 * fraction;
        });
      } else if (trail.timer >= SAMPLE_INTERVAL) {
        trail.timer %= SAMPLE_INTERVAL;
        const last = history[history.length - 1];
        const dx = position.x - last.x;
        const dz = position.z - last.z;
        const distance = Math.hypot(dx, dz);
        if (distance > 150) {
          history.length = 0;
          history.push({x: position.x, z: position.z, t: this._time});
          this._disposeParticleSystem(trail);
          trail.moving = false;
        } else if (distance > Math.sqrt(0.5)) {
          trail.flowX = dx / distance;
          trail.flowZ = dz / distance;
          trail.speed = Math.min(distance / SAMPLE_INTERVAL, 100);
          // Subdivide a fresh sample so tight turns do not produce long wedges.
          const samples = Math.min(8, Math.max(2, Math.ceil(distance / 2)));
          for (let i = 1; i <= samples; i++) history.push({
            x: last.x + dx * i / samples, z: last.z + dz * i / samples,
            t: this._time - SAMPLE_INTERVAL * (1 - i / samples),
          });
          while (history.length > MAX_HISTORY) history.shift();
          trail.moving = true;
        } else trail.moving = false;
        trail.dirty = true;
      }
      // Bound both elapsed age and world distance, including a stationary bot.
      while (history.length > 1 && (this._time - history[0].t > MAX_WAKE_AGE
          || Math.hypot(position.x - history[0].x, position.z - history[0].z) > MAX_WAKE_LENGTH)) {
        history.shift();
        trail.dirty = true;
      }
      if (reducedMotion) this._disposeParticleSystem(trail);
      else if (style.particles && !trail.particles) {
        trail.particles = this._createParticleSystem(botId, entry, style);
      }
      if (trail.particles) {
        const particles = trail.particles;
        const config = style.particles;
        // A world-space emitter avoids rotated/scaled chassis distorting wakes.
        particles.emitter = position;
        const jet = config.motion === 'jet' ? 5 : Math.min(trail.speed * 0.045, 3);
        particles.direction1.set(-trail.flowX * jet - config.spread, config.rise * 0.6,
          -trail.flowZ * jet - config.spread);
        particles.direction2.set(-trail.flowX * jet + config.spread, config.rise,
          -trail.flowZ * jet + config.spread);
        particles.emitRate = trail.moving ? config.emitRate : 0;
      }
      if (history.length < 2) {
        trail.mesh?.setEnabled(false);
        continue;
      }
      if (trail.dirty || trail.geometryTimer >= GEOMETRY_INTERVAL) {
        trail.geometryTimer %= GEOMETRY_INTERVAL;
        this._drawFilament(trail, entry, reducedMotion);
        trail.dirty = false;
      }
      trail.mesh?.setEnabled(true);
    }
    for (const [botId, trail] of this.trails) {
      if (!seen.has(botId)) {
        trail.mesh?.dispose();
        this._disposeParticleSystem(trail);
        this.trails.delete(botId);
      }
    }
  }

  _drawFilament(trail, entry, reducedMotion) {
    const B = window.BABYLON;
    const {style, history} = trail;
    const n = history.length;
    for (let i = 0; i < MAX_HISTORY; i++) {
      const index = Math.min(i, n - 1);
      const point = history[index];
      const previous = history[Math.max(0, index - 1)];
      const next = history[Math.min(n - 1, index + 1)];
      const dx = next.x - previous.x;
      const dz = next.z - previous.z;
      const length = Math.hypot(dx, dz) || 1;
      const px = -dz / length;
      const pz = dx / length;
      const age = Math.max(0, this._time - point.t);
      const fade = Math.max(0, 1 - age / MAX_WAKE_AGE);
      const taper = Math.sin(Math.PI * index / (n - 1));
      const curl = reducedMotion ? 0 : Math.sin(age * style.frequency * 5 + point.t * 3)
        * style.curl * Math.sin(age * Math.PI);
      const width = i < n ? style.filament * style.width * taper * fade : 0;
      const y = TRAIL_Y + style.lift * Math.sin(age * Math.PI);
      trail.left[i].set(point.x + px * (curl + width), y, point.z + pz * (curl + width));
      trail.right[i].set(point.x + px * (curl - width), y, point.z + pz * (curl - width));
    }
    if (!trail.mesh) {
      const ribbon = B.MeshBuilder.CreateRibbon(`trail-${trail.botId}`, {
        pathArray: [trail.left, trail.right], updatable: true,
        sideOrientation: B.Mesh.DOUBLESIDE,
      }, this.scene);
      ribbon.material = this._getSharedRibbonMaterial();
      ribbon.isPickable = false;
      ribbon.hasVertexAlpha = true;
      ribbon.freezeNormals();
      trail.mesh = ribbon;
      trail.colors = new Float32Array(ribbon.getTotalVertices() * 4);
      ribbon.setVerticesData(B.VertexBuffer.ColorKind, trail.colors, true);
    } else B.MeshBuilder.CreateRibbon(null, {
      pathArray: [trail.left, trail.right], instance: trail.mesh,
    });
    let {primary, secondary} = this._getStyleColors(style);
    if (style.key === 'standard' && entry.bodyMat?.diffuseColor) {
      primary = secondary = entry.bodyMat.diffuseColor;
    }
    const brightness = isEnabled('movementTrails', 'trailBrightness')
      || style.key !== 'standard' || this.options.forceEnabled === true ? 1 : 0.55;
    for (let vertex = 0; vertex < trail.mesh.getTotalVertices(); vertex++) {
      const index = vertex % MAX_HISTORY;
      const point = history[Math.min(index, n - 1)];
      const fade = Math.max(0, 1 - (this._time - point.t) / MAX_WAKE_AGE);
      const amount = fade * fade;
      const offset = vertex * 4;
      trail.colors[offset] = (primary.r + (secondary.r - primary.r) * amount) * brightness;
      trail.colors[offset + 1] = (primary.g + (secondary.g - primary.g) * amount) * brightness;
      trail.colors[offset + 2] = (primary.b + (secondary.b - primary.b) * amount) * brightness;
      trail.colors[offset + 3] = index < n ? amount * style.alpha : 0;
    }
    trail.mesh.updateVerticesData(B.VertexBuffer.ColorKind, trail.colors);
  }

  dispose() {
    for (const [, trail] of this.trails) {
      if (trail.mesh) trail.mesh.dispose();
      this._disposeParticleSystem(trail);
    }
    this.trails.clear();
    if (this.sharedRibbonMaterial) this.sharedRibbonMaterial.dispose();
    if (this.sharedParticleTexture) this.sharedParticleTexture.dispose();
    this.sharedRibbonMaterial = null;
    this.sharedParticleTexture = null;
    this._styleColors.clear();
    this._renderQueue.length = 0;
    this._paidCandidates.length = 0;
    this._standardCandidates.length = 0;
  }
}
