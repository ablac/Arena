'use strict';

/**
 * Movement trail system — bounded cosmetic ribbons and particle wakes.
 * Ribbons reuse one material, one mesh per visible bot, and fixed-size buffers.
 * Particle styles share one procedural texture and have a separate hard cap.
 * @module renderer/trails
 */

import { isEnabled } from '../settings.js';

const MAX_HISTORY = 18;
// Spectator positions arrive at 10 Hz. Sampling three times faster rebuilt the
// same ribbon geometry from interpolated points without adding useful truth.
const SAMPLE_INTERVAL = 0.1;
const MAX_RENDERED_TRAILS = 48;
const MAX_PARTICLE_SYSTEMS = 24;
const MAX_PARTICLES_PER_TRAIL = 28;
const TRAIL_SELECTION_HYSTERESIS_SQ = 40 * 40;
const TRAIL_WIDTH = 6;
const TRAIL_Y = 0.4;

const STANDARD_STYLE = Object.freeze({
  key: 'standard',
  primary: '#63d8ff',
  secondary: '#b9f3ff',
  width: 0.72,
  alpha: 0.22,
  particles: null,
});

function particleStyle(emitRate, gravityY, options = {}) {
  return Object.freeze({
    emitRate,
    gravityY,
    minSize: options.minSize ?? 0.45,
    maxSize: options.maxSize ?? 1.25,
    minLife: options.minLife ?? 0.24,
    maxLife: options.maxLife ?? 0.72,
    spread: options.spread ?? 1.6,
    rise: options.rise ?? 0,
  });
}

function trailStyle(key, primary, secondary, width, alpha, particles) {
  // Paid trails must remain identifiable from spectator zoom, not merely tint
  // the free wake. Scale each authored signature while retaining its relative
  // width/opacity/emission character and the renderer's existing hard caps.
  const emphasizedParticles = particles && Object.freeze({
    ...particles,
    emitRate: Math.max(24, particles.emitRate * 1.5),
  });
  return Object.freeze({
    key,
    primary,
    secondary,
    width: Math.max(1.2, width * 1.35),
    alpha: Math.max(0.58, alpha * 1.55),
    particles: emphasizedParticles,
  });
}

// Every server-provided asset key resolves through this fixed local allowlist.
// Styles are intentionally data-only so the shop preview and live arena use
// exactly the same presentation without loading remote scripts or textures.
const TRAIL_STYLES = Object.freeze({
  ember_sparks: trailStyle('ember_sparks', '#ff5b2e', '#ffd166', 0.86, 0.34, particleStyle(20, 2.8, {minLife: 0.18, maxLife: 0.52})),
  frost_shards: trailStyle('frost_shards', '#7be7ff', '#e5fbff', 0.92, 0.32, particleStyle(14, -1.8, {minSize: 0.35, maxSize: 0.95})),
  ion_stream: trailStyle('ion_stream', '#39f5ff', '#4b7cff', 0.72, 0.4, particleStyle(18, 0.4, {spread: 0.75, minLife: 0.3, maxLife: 0.82})),
  plasma_ribbon: trailStyle('plasma_ribbon', '#ff3fd1', '#7957ff', 1.12, 0.4, particleStyle(12, 0.8, {minSize: 0.7, maxSize: 1.5})),
  void_motes: trailStyle('void_motes', '#6d43c5', '#cc8cff', 1.02, 0.28, particleStyle(10, 1.6, {spread: 2.2, minLife: 0.45, maxLife: 1.05})),
  solar_wake: trailStyle('solar_wake', '#ff9e2c', '#fff4a8', 1.18, 0.4, particleStyle(19, 2.1, {minSize: 0.6, maxSize: 1.55})),
  lunar_dust: trailStyle('lunar_dust', '#aeb9da', '#ffffff', 0.96, 0.3, particleStyle(12, -0.7, {spread: 2.4, minLife: 0.5, maxLife: 1.1})),
  comet_tail: trailStyle('comet_tail', '#6ee7ff', '#ffffff', 1.3, 0.42, particleStyle(22, -0.35, {spread: 0.7, minLife: 0.24, maxLife: 0.68})),
  nebula_pulse: trailStyle('nebula_pulse', '#8f63ff', '#ff77cc', 1.18, 0.36, particleStyle(13, 0.9, {spread: 2.1, minSize: 0.75, maxSize: 1.65})),
  storm_arcs: trailStyle('storm_arcs', '#56b7ff', '#e8fbff', 0.84, 0.44, particleStyle(23, -2.4, {minSize: 0.25, maxSize: 0.78, minLife: 0.12, maxLife: 0.34})),
  static_glitch: trailStyle('static_glitch', '#00f0b5', '#f638dc', 0.76, 0.4, particleStyle(21, 0, {spread: 2.8, minSize: 0.25, maxSize: 0.82, minLife: 0.1, maxLife: 0.3})),
  pixel_scatter: trailStyle('pixel_scatter', '#57f287', '#78a7ff', 0.74, 0.34, particleStyle(17, -2.6, {minSize: 0.28, maxSize: 0.72, minLife: 0.28, maxLife: 0.7})),
  data_stream: trailStyle('data_stream', '#39ffb6', '#83f7ff', 0.68, 0.42, particleStyle(20, -1.2, {spread: 0.55, minSize: 0.22, maxSize: 0.58})),
  holo_prism: trailStyle('holo_prism', '#64e6ff', '#ff72d2', 1.02, 0.38, particleStyle(14, 0.5, {minSize: 0.6, maxSize: 1.35})),
  toxic_spores: trailStyle('toxic_spores', '#9bea37', '#e3ff75', 0.98, 0.34, particleStyle(15, 2.3, {spread: 2.7, minLife: 0.55, maxLife: 1.2})),
  verdant_leaves: trailStyle('verdant_leaves', '#35c96f', '#b8f26d', 0.9, 0.32, particleStyle(12, -3.2, {spread: 2.5, minSize: 0.45, maxSize: 1.05, minLife: 0.5, maxLife: 1.15})),
  sand_wake: trailStyle('sand_wake', '#c99a55', '#f0d58d', 1.14, 0.3, particleStyle(18, -3.6, {spread: 2.9, minLife: 0.38, maxLife: 0.95})),
  magma_cinders: trailStyle('magma_cinders', '#ff3d20', '#ffbf3f', 1.08, 0.4, particleStyle(19, 3.4, {minSize: 0.3, maxSize: 0.92, minLife: 0.35, maxLife: 0.88})),
  ocean_spray: trailStyle('ocean_spray', '#23aef3', '#b9fbff', 1.08, 0.36, particleStyle(18, -3, {spread: 2.25, minSize: 0.38, maxSize: 1.08})),
  gilded_dust: trailStyle('gilded_dust', '#dcae36', '#fff0a1', 0.92, 0.4, particleStyle(16, -0.8, {spread: 2.1, minLife: 0.48, maxLife: 1.05})),
  rune_sparks: trailStyle('rune_sparks', '#9a6cff', '#60e9ff', 0.96, 0.42, particleStyle(13, 1.8, {spread: 1.9, minSize: 0.5, maxSize: 1.1})),
  phantom_smoke: trailStyle('phantom_smoke', '#766b99', '#c8bce8', 1.24, 0.25, particleStyle(10, 2.5, {spread: 2.5, minSize: 0.9, maxSize: 1.85, minLife: 0.7, maxLife: 1.3})),
  gear_sparks: trailStyle('gear_sparks', '#d67b31', '#f7df92', 0.82, 0.38, particleStyle(20, -3.1, {minSize: 0.28, maxSize: 0.82, minLife: 0.2, maxLife: 0.55})),
  bounty_flare: trailStyle('bounty_flare', '#ffca3a', '#ff5a36', 1.16, 0.44, particleStyle(18, 2.7, {spread: 1.4, minSize: 0.6, maxSize: 1.4})),
});

/** Resolve an untrusted cosmetic key to one local, bounded style. */
export function resolveTrailStyle(assetKey) {
  if (typeof assetKey !== 'string') return STANDARD_STYLE;
  return TRAIL_STYLES[assetKey.trim().toLowerCase()] || STANDARD_STYLE;
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
    this._motionQuery = typeof window.matchMedia === 'function'
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null;
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
    gradient.addColorStop(0.38, 'rgba(255,255,255,0.9)');
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
    colors = Object.freeze({
      primary: parseColor(B, style.primary, STANDARD_STYLE.primary),
      secondary: parseColor(B, style.secondary, STANDARD_STYLE.secondary),
    });
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
    particles.minEmitBox = new B.Vector3(-0.8, 0.8, -1.6);
    particles.maxEmitBox = new B.Vector3(0.8, 2.8, 1.6);
    particles.direction1 = new B.Vector3(-style.particles.spread, style.particles.rise, -style.particles.spread);
    particles.direction2 = new B.Vector3(style.particles.spread, style.particles.rise, style.particles.spread);
    particles.gravity = new B.Vector3(0, style.particles.gravityY, 0);
    particles.minSize = style.particles.minSize;
    particles.maxSize = style.particles.maxSize;
    particles.minLifeTime = style.particles.minLife;
    particles.maxLifeTime = style.particles.maxLife;
    particles.emitRate = 0;
    particles.minAngularSpeed = -2.4;
    particles.maxAngularSpeed = 2.4;
    particles.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    particles.updateSpeed = 0.012;
    this._applyParticleColors(particles, style);
    particles.start();
    this.particleSystemCount += 1;
    return particles;
  }

  _applyParticleColors(particles, style) {
    if (!particles) return;
    const B = window.BABYLON;
    const {primary, secondary} = this._getStyleColors(style);
    particles.color1 = new B.Color4(primary.r, primary.g, primary.b, 0.92);
    particles.color2 = new B.Color4(secondary.r, secondary.g, secondary.b, 0.78);
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

  /** Break every existing ribbon at the latest snapped bot position. */
  reset(botEntries) {
    for (const [botId, trail] of this.trails) {
      const entry = botEntries?.get(botId);
      trail.timer = 0;
      trail.history.length = 0;
      if (entry?._interpReady) {
        const position = entryPosition(entry);
        if (position && this.options.previewPath === true) {
          trail.history.push({x: position.x - 15, z: position.z}, {x: position.x, z: position.z});
        } else if (position) {
          trail.history.push({x: position.x, z: position.z});
        }
      }
      trail.dirty = this.options.previewPath === true;
      trail.moving = this.options.staticPreview === true;
      this._hideTrail(trail);
    }
  }

  _createTrail(botId, entry, x, z, style) {
    const B = window.BABYLON;
    const left = [];
    const right = [];
    for (let i = 0; i < MAX_HISTORY; i++) {
      left.push(new B.Vector3(x, TRAIL_Y, z));
      right.push(new B.Vector3(x, TRAIL_Y, z));
    }
    const seededPreview = this.options.staticPreview === true || this.options.previewPath === true;
    const history = seededPreview
      ? [{x: x - 15, z}, {x, z}]
      : [{x, z}];
    return {
      history,
      mesh: null,
      particles: null,
      style,
      timer: 0,
      left,
      right,
      colors: null,
      dirty: seededPreview,
      moving: this.options.staticPreview === true,
      entry,
      botId,
    };
  }

  _updateStyle(trail, entry, style) {
    if (trail.style.key === style.key) return;
    this._disposeParticleSystem(trail);
    trail.style = style;
    trail.entry = entry;
    trail.dirty = true;
  }

  _buildRenderQueue(botEntries) {
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
    // purchased trail. A small existing-trail bonus prevents edge thrash.
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

  /**
   * Called every render frame with the bot renderer's entries map.
   * @param {Map<string, Object>|null} botEntries
   * @param {number} dt
   */
  render(botEntries, dt) {
    if (!botEntries) return;

    const suspended = pageHidden();
    const reducedMotion = this._reducedMotion();
    if (!this._enabled() || suspended) {
      for (const [, trail] of this.trails) this._hideTrail(trail);
      return;
    }

    const B = window.BABYLON;
    const seen = new Set();

    for (const botId of this._buildRenderQueue(botEntries)) {
      const entry = botEntries.get(botId);
      const style = resolveTrailStyle(cosmeticTrailKey(entry));
      seen.add(botId);

      const position = entryPosition(entry);
      if (!position) continue;
      const x = position.x;
      const z = position.z;
      let trail = this.trails.get(botId);
      if (!trail) {
        trail = this._createTrail(botId, entry, x, z, style);
        this.trails.set(botId, trail);
      } else {
        this._updateStyle(trail, entry, style);
      }

      trail.timer += Number.isFinite(dt) ? Math.max(0, dt) : 0;
      if (this.options.staticPreview === true) {
        trail.moving = true;
      } else if (trail.timer >= SAMPLE_INTERVAL) {
        trail.timer %= SAMPLE_INTERVAL;
        const last = trail.history[trail.history.length - 1];
        const dx = x - last.x;
        const dz = z - last.z;
        const distanceSquared = dx * dx + dz * dz;
        if (distanceSquared > 150 * 150) {
          trail.history.length = 0;
          trail.history.push({x, z});
          trail.dirty = true;
          trail.moving = false;
        } else if (distanceSquared > 0.5) {
          trail.history.push({x, z});
          if (trail.history.length > MAX_HISTORY) trail.history.shift();
          trail.dirty = true;
          trail.moving = true;
        } else {
          trail.moving = false;
        }
      }

      if (style.particles && !reducedMotion && !trail.particles) {
        trail.particles = this._createParticleSystem(botId, entry, style);
      }
      if (trail.particles) {
        trail.particles.emitter = entry.root;
        trail.particles.emitRate = trail.moving && !reducedMotion
          ? style.particles.emitRate
          : 0;
      }

      if (trail.history.length < 2) {
        if (trail.mesh) trail.mesh.setEnabled(false);
        continue;
      }
      if (trail.mesh && !trail.mesh.isEnabled()) trail.mesh.setEnabled(true);
      if (!trail.dirty && trail.mesh) continue;
      trail.dirty = false;

      const hist = trail.history;
      const n = hist.length;
      for (let i = 0; i < n; i++) {
        let nx;
        let nz;
        if (i < n - 1) {
          nx = hist[i + 1].x - hist[i].x;
          nz = hist[i + 1].z - hist[i].z;
        } else {
          nx = hist[i].x - hist[i - 1].x;
          nz = hist[i].z - hist[i - 1].z;
        }
        const len = Math.sqrt(nx * nx + nz * nz) || 1;
        const px = -nz / len;
        const pz = nx / len;
        const alpha = i / (n - 1);
        const width = TRAIL_WIDTH * style.width * alpha;
        trail.left[i].set(hist[i].x + px * width, TRAIL_Y, hist[i].z + pz * width);
        trail.right[i].set(hist[i].x - px * width, TRAIL_Y, hist[i].z - pz * width);
      }
      for (let i = n; i < MAX_HISTORY; i++) {
        trail.left[i].copyFrom(trail.left[n - 1]);
        trail.right[i].copyFrom(trail.right[n - 1]);
      }

      try {
        if (!trail.mesh) {
          const ribbon = B.MeshBuilder.CreateRibbon(`trail-${botId}`, {
            pathArray: [trail.left, trail.right],
            updatable: true,
            sideOrientation: B.Mesh.DOUBLESIDE,
          }, this.scene);
          ribbon.material = this._getSharedRibbonMaterial();
          ribbon.isPickable = false;
          ribbon.hasVertexAlpha = true;
          trail.mesh = ribbon;
          trail.colors = new Float32Array(ribbon.getTotalVertices() * 4);
        } else {
          B.MeshBuilder.CreateRibbon(null, {
            pathArray: [trail.left, trail.right],
            instance: trail.mesh,
          });
        }

        const bright = isEnabled('movementTrails', 'trailBrightness');
        let primary;
        let secondary;
        if (style.key === 'standard' && entry.bodyMat?.diffuseColor) {
          primary = entry.bodyMat.diffuseColor;
          secondary = entry.bodyMat.diffuseColor;
        } else {
          ({primary, secondary} = this._getStyleColors(style));
        }
        const brightness = bright || this.options.forceEnabled === true ? 1 : 0.55;
        const vertexCount = trail.mesh.getTotalVertices();
        for (let vertex = 0; vertex < vertexCount; vertex++) {
          const index = vertex % MAX_HISTORY;
          const amount = index < n ? index / (n - 1) : 0;
          const red = primary.r + (secondary.r - primary.r) * amount;
          const green = primary.g + (secondary.g - primary.g) * amount;
          const blue = primary.b + (secondary.b - primary.b) * amount;
          trail.colors[vertex * 4] = red * brightness;
          trail.colors[vertex * 4 + 1] = green * brightness;
          trail.colors[vertex * 4 + 2] = blue * brightness;
          trail.colors[vertex * 4 + 3] = index < n ? amount * style.alpha : 0;
        }
        trail.mesh.setVerticesData(B.VertexBuffer.ColorKind, trail.colors, true);
      } catch {
        // A transient degenerate path is safe to skip; the next sample reuses
        // the same buffers and retries without allocating another mesh.
      }
    }

    for (const [botId, trail] of this.trails) {
      if (!seen.has(botId)) {
        if (trail.mesh) trail.mesh.dispose();
        this._disposeParticleSystem(trail);
        this.trails.delete(botId);
      }
    }
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
