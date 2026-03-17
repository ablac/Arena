'use strict';

/**
 * Visual projectile system — arrows (bow) and energy bolts (staff).
 * Uses Babylon.js Animation API for self-managed projectile motion.
 * @module renderer/projectiles
 */

const CONFIGS = {
  bow:   { travelTime: 0.25, meshType: 'arrow', color: [0.9, 0.85, 0.7], arc: 3 },
  staff: { travelTime: 0.35, meshType: 'bolt',  color: [0.5, 0.2, 1.0],  arc: 5 },
};

let _counter = 0;

export class ProjectileRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Array<BABYLON.Animatable>} */
    this.activeAnimatables = [];
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
   */
  spawn(fromX, fromZ, toX, toZ, weapon, hexColor, onImpact) {
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

    mesh.position.set(fromX, 12, fromZ);

    // Orient toward target
    const dx = toX - fromX;
    const dz = toZ - fromZ;
    mesh.rotation.y = Math.atan2(dx, dz);
    if (cfg.meshType === 'arrow') mesh.rotation.x = Math.PI / 2;

    // Build position animation with arc via 3 keyframes
    const totalFrames = Math.round(cfg.travelTime * 60);
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

    const animatable = this.scene.beginDirectAnimation(
      mesh, [posAnim], 0, totalFrames, false, 1
    );

    this.activeAnimatables.push(animatable);

    animatable.onAnimationEnd = () => {
      if (onImpact) onImpact();
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
  }
}
