'use strict';

/**
 * Visual projectile system — arrows (bow) and energy bolts (staff).
 * Uses Babylon.js Animation API for self-managed projectile motion.
 * @module renderer/projectiles
 */

const CONFIGS = {
  bow:   { meshType: 'arrow', color: [0.9, 0.85, 0.7], arc: 1.8, worldSpeed: 300, trailRate: 22 },
  staff: { meshType: 'bolt',  color: [0.5, 0.2, 1.0],  arc: 3.5, worldSpeed: 140, trailRate: 28 },
};

let _counter = 0;

const TRAIL_TEX_DATA = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAACXBIWXMAAAsSAAALEgHS3X78AAAAxklEQVRYhe2WwQ3CMAxFz2YwAmNwAmMwAmMwAmOwAhM0AiNwAhM0wggqjN0vSZIl/1N7pS1+8gNR5tZ4vYgA4G6v1j4qAABvABeAK8w+3lW0hQfW9a4uGQBM5j8AyGGbGwCd5uB1Vg4g2h8A8lJf2A3wSEmQ4T4kz1y5iD5Sbnx8AaY2mN+G2R8B5V7n6qVq5d3AYQ5xjM1XoI2fJ0Q3i3xL6Y4K3bYQmY2ot7b8Tf1k9U4A1uZyD2a7J4Qf8bqg8dgfQDG2h7U+Lf6HwAAAABJRU5ErkJggg==';

export class ProjectileRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Array<BABYLON.Animatable>} */
    this.activeAnimatables = [];
    // Trail texture is shared by every projectile: decoding the base64 PNG
    // and uploading it to the GPU per shot was pure churn.
    this._trailTex = null;
  }

  /** @private True when the shared texture has been disposed by Babylon. */
  _isTrailTextureDisposed() {
    if (!this._trailTex) return true;
    const disposed = this._trailTex.isDisposed;
    return typeof disposed === 'function' ? disposed.call(this._trailTex) : disposed === true;
  }

  /** @private Lazily create the shared trail texture. */
  _getTrailTexture() {
    const B = window.BABYLON;
    if (this._isTrailTextureDisposed()) {
      this._trailTex = new B.Texture(TRAIL_TEX_DATA, this.scene, false, false, B.Texture.BILINEAR_SAMPLINGMODE);
    }
    return this._trailTex;
  }

  /**
   * Spawn a visual projectile from attacker to target.
   * @param {number} fromX
   * @param {number} fromZ
   * @param {number} toX
   * @param {number} toZ
   * @param {string} weapon - 'bow' or 'staff'
   * @param {string} hexColor - attacker's avatar color
   * @param {Function} [onImpact] - called when projectile reaches target
   * @param {Object} [options]
   * @param {number} [options.travelTime] - override travel time in seconds
   */
  spawn(fromX, fromZ, toX, toZ, weapon, hexColor, onImpact, options = {}) {
    const cfg = CONFIGS[weapon];
    if (!cfg) return;

    const B = window.BABYLON;
    const id = ++_counter;
    let mesh;

    if (cfg.meshType === 'arrow') {
      mesh = B.MeshBuilder.CreateCylinder(`proj-${id}`, {
        height: 6, diameter: 0.5, tessellation: 4
      }, this.scene);
    } else {
      mesh = B.MeshBuilder.CreateSphere(`proj-${id}`, {
        diameter: 3, segments: 4
      }, this.scene);
    }

    const mat = new B.StandardMaterial(`pmat-${id}`, this.scene);
    mat.emissiveColor = new B.Color3(cfg.color[0], cfg.color[1], cfg.color[2]);
    mat.disableLighting = true;
    mesh.material = mat;
    const intensity = Number.isFinite(options.intensity) ? options.intensity : 1;
    if (intensity > 1 && weapon === 'bow') {
      const scale = Math.min(1.55, 1 + (intensity - 1) * 0.45);
      mesh.scaling.set(scale, scale, scale);
      mat.emissiveColor = mat.emissiveColor.scale(1 + Math.min(0.5, (intensity - 1) * 0.35));
    }

    mesh.position.set(fromX, 12, fromZ);

    // Orient toward target
    const dx = toX - fromX;
    const dz = toZ - fromZ;
    mesh.rotation.y = Math.atan2(dx, dz);
    if (cfg.meshType === 'arrow') mesh.rotation.x = Math.PI / 2;

    const distance = Math.hypot(dx, dz);
    const travelTime = Math.max(
      0.12,
      Number.isFinite(options.travelTime) ? options.travelTime : distance / Math.max(cfg.worldSpeed || 30, 1),
    );
    const totalFrames = Math.max(12, Math.round(travelTime * 60));
    const midFrame = Math.round(totalFrames / 2);
    const midX = (fromX + toX) / 2;
    const midZ = (fromZ + toZ) / 2;

    const posAnim = new B.Animation(
      `projAnim-${id}`,
      'position',
      60,
      B.Animation.ANIMATIONTYPE_VECTOR3,
      B.Animation.ANIMATIONLOOPMODE_CONSTANT
    );

    posAnim.setKeys([
      { frame: 0,          value: new B.Vector3(fromX, 12, fromZ) },
      { frame: midFrame,   value: new B.Vector3(midX, 12 + cfg.arc, midZ) },
      { frame: totalFrames, value: new B.Vector3(toX, 12, toZ) },
    ]);

    const trail = new B.ParticleSystem(`proj-trail-${id}`, weapon === 'staff' ? 40 : 24, this.scene);
    trail.emitter = mesh;
    trail.particleTexture = this._getTrailTexture();
    trail.minLifeTime = weapon === 'staff' ? 0.16 : 0.1;
    trail.maxLifeTime = weapon === 'staff' ? 0.28 : 0.18;
    trail.minSize = weapon === 'staff' ? 1.8 : 1.0;
    trail.maxSize = weapon === 'staff' ? 3.6 : 2.2;
    trail.emitRate = cfg.trailRate;
    if (weapon === 'bow' && intensity > 1) {
      trail.minSize *= Math.min(1.45, 1 + (intensity - 1) * 0.25);
      trail.maxSize *= Math.min(1.55, 1 + (intensity - 1) * 0.3);
      trail.emitRate = Math.round(trail.emitRate * Math.min(1.35, 1 + (intensity - 1) * 0.2));
    }
    trail.minEmitPower = 0.1;
    trail.maxEmitPower = 0.8;
    trail.color1 = new B.Color4(cfg.color[0], cfg.color[1], cfg.color[2], weapon === 'staff' ? 0.8 : 0.55);
    trail.color2 = new B.Color4(1, 1, 1, weapon === 'staff' ? 0.6 : 0.25);
    trail.colorDead = new B.Color4(cfg.color[0] * 0.2, cfg.color[1] * 0.2, cfg.color[2] * 0.2, 0);
    trail.blendMode = B.ParticleSystem.BLENDMODE_ADD;
    trail.direction1 = new B.Vector3(-0.4, -0.2, -0.4);
    trail.direction2 = new B.Vector3(0.4, 0.2, 0.4);
    trail.start();

    const animatable = this.scene.beginDirectAnimation(
      mesh, [posAnim], 0, totalFrames, false, 1
    );

    this.activeAnimatables.push(animatable);

    animatable.onAnimationEnd = () => {
      if (onImpact) onImpact();
      // false: the particle texture is shared across all projectiles.
      trail.dispose(false);
      mesh.dispose();
      mat.dispose();
      const idx = this.activeAnimatables.indexOf(animatable);
      if (idx !== -1) this.activeAnimatables.splice(idx, 1);
    };
  }

  /**
   * Retained for interface compatibility.
   * Animations are self-managed by the Babylon.js Animation API.
   * @param {number} dt - frame delta in seconds
   */
  update(dt) {
    // No-op: projectile motion is handled by Babylon.js animations
  }

  dispose() {
    for (const anim of this.activeAnimatables) {
      anim.stop();
      const target = anim.target;
      if (target && typeof target.dispose === 'function') target.dispose();
      if (target && target.material && typeof target.material.dispose === 'function') {
        target.material.dispose();
      }
    }
    this.activeAnimatables = [];
    if (this._trailTex) {
      this._trailTex.dispose();
      this._trailTex = null;
    }
  }
}
