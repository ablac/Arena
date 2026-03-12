'use strict';

/**
 * Pickup rendering — floating, rotating collectibles with glow.
 * Health=green cross, speed=yellow bolt, damage=red diamond, shield=blue disc.
 * @module renderer/pickups
 */

import { makeMat } from './utils.js';

const COLORS = {
  health:  [0.1, 1.0, 0.3],
  speed:   [1.0, 1.0, 0.1],
  damage:  [1.0, 0.25, 0.2],
  shield:  [0.25, 0.5, 1.0],
};

export class PickupRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.meshes = new Map();
    this._time = 0;
  }

  /**
   * Update pickups from state.
   * @param {Array} pickups - [{ pickup_id, type, position }]
   */
  update(pickups) {
    const seen = new Set();
    this._time += 0.04;

    for (const p of pickups) {
      seen.add(p.pickup_id);
      let entry = this.meshes.get(p.pickup_id);
      if (!entry) {
        entry = this._create(p);
        this.meshes.set(p.pickup_id, entry);
      }

      const x = p.position[0], z = p.position[1];
      const floatY = 8 + Math.sin(this._time * 2 + x * 0.01) * 3;
      entry.root.position.set(x, floatY, z);
      // Slow rotation
      entry.root.rotation.y = this._time * 1.5 + x * 0.1;
      // Pulse scale
      const pulse = 0.9 + Math.sin(this._time * 3 + z * 0.01) * 0.15;
      entry.root.scaling.setAll(pulse);
      // Glow pulse
      entry.glowMat.alpha = 0.15 + Math.sin(this._time * 4 + x) * 0.1;
    }

    // Remove collected
    for (const [id, entry] of this.meshes) {
      if (!seen.has(id)) {
        this._dispose(entry);
        this.meshes.delete(id);
      }
    }
  }

  /** @private Create pickup meshes based on type. */
  _create(p) {
    const B = window.BABYLON;
    const id = p.pickup_id;
    const type = p.type || 'health';
    const c = COLORS[type] || COLORS.health;
    const color = new B.Color3(c[0], c[1], c[2]);

    const root = new B.TransformNode(`pu-${id}`, this.scene);

    // Main shape varies by type
    let mesh;
    if (type === 'health') {
      // Green cross from two boxes
      const h = B.MeshBuilder.CreateBox(`puh-${id}`, { width: 3, height: 8, depth: 3 }, this.scene);
      const v = B.MeshBuilder.CreateBox(`puv-${id}`, { width: 8, height: 3, depth: 3 }, this.scene);
      h.parent = root; v.parent = root;
      const mat = makeMat(`pumat-${id}`, this.scene, color, { emissiveFactor: 0.8, noLight: true });
      h.material = mat; v.material = mat;
      mesh = h; mesh._sibling = v; mesh._mat = mat;
    } else if (type === 'shield') {
      mesh = B.MeshBuilder.CreateSphere(`pum-${id}`, { diameter: 8, segments: 8 }, this.scene);
      mesh.parent = root;
      mesh._mat = makeMat(`pumat-${id}`, this.scene, color, { emissiveFactor: 0.7, noLight: true, alpha: 0.8 });
      mesh.material = mesh._mat;
    } else {
      // Diamond shape
      mesh = B.MeshBuilder.CreatePolyhedron(`pum-${id}`, { type: 1, size: 4 }, this.scene);
      mesh.parent = root;
      mesh._mat = makeMat(`pumat-${id}`, this.scene, color, { emissiveFactor: 0.8, noLight: true });
      mesh.material = mesh._mat;
    }

    // Glow halo disc
    const glow = B.MeshBuilder.CreateDisc(`pug-${id}`, { radius: 12, tessellation: 16 }, this.scene);
    glow.rotation.x = Math.PI / 2;
    glow.position.y = -2;
    glow.parent = root;
    const glowMat = makeMat(`pugm-${id}`, this.scene, color, {
      noLight: true, alpha: 0.2, emissiveFactor: 1
    });
    glow.material = glowMat;

    return { root, mesh, glow, glowMat };
  }

  /** @private */
  _dispose(entry) {
    if (entry.mesh._sibling) entry.mesh._sibling.dispose();
    if (entry.mesh._mat) entry.mesh._mat.dispose();
    entry.mesh.dispose();
    entry.glow.dispose();
    entry.glowMat.dispose();
    entry.root.dispose();
  }
}
