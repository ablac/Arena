'use strict';

/**
 * Visual projectile system — arrows (bow) and energy bolts (staff).
 * Projectiles lerp from attacker to target with weapon-specific visuals.
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
    /** @type {Array<Object>} */
    this.active = [];
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

    this.active.push({
      mesh, mat, fromX, fromZ, toX, toZ,
      progress: 0,
      travelTime: cfg.travelTime,
      arc: cfg.arc,
      onImpact: onImpact || null,
    });
  }

  /**
   * Tick all active projectiles. Called every render frame.
   * @param {number} dt - frame delta in seconds
   */
  update(dt) {
    for (let i = this.active.length - 1; i >= 0; i--) {
      const p = this.active[i];
      p.progress += dt / p.travelTime;

      if (p.progress >= 1) {
        if (p.onImpact) p.onImpact();
        p.mesh.dispose();
        p.mat.dispose();
        this.active.splice(i, 1);
        continue;
      }

      // Linear interpolation with vertical arc
      p.mesh.position.x = p.fromX + (p.toX - p.fromX) * p.progress;
      p.mesh.position.z = p.fromZ + (p.toZ - p.fromZ) * p.progress;
      p.mesh.position.y = 12 + Math.sin(p.progress * Math.PI) * p.arc;
    }
  }

  dispose() {
    for (const p of this.active) {
      p.mesh.dispose();
      p.mat.dispose();
    }
    this.active = [];
  }
}
