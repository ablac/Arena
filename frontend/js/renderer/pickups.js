'use strict';

/**
 * Pickup rendering — floating, rotating collectibles with glow.
 * Materials shared by pickup type for performance.
 * @module renderer/pickups
 */

import { makeMat } from './utils.js';

const COLORS = {
  health_pack:    [0.1, 1.0, 0.3],
  speed_boost:    [1.0, 1.0, 0.1],
  damage_boost:   [1.0, 0.25, 0.2],
  shield_bubble:  [0.25, 0.5, 1.0],
};

/** @type {Map<string, {shapeMat: BABYLON.StandardMaterial, glowMat: BABYLON.StandardMaterial}>} */
const _typeMats = new Map();

function _getMats(type, scene) {
  let mats = _typeMats.get(type);
  if (mats && !mats.shapeMat.isDisposed) return mats;
  const B = window.BABYLON;
  const c = COLORS[type] || COLORS.health_pack;
  const color = new B.Color3(c[0], c[1], c[2]);
  const shapeMat = makeMat(`pumat-${type}`, scene, color, { emissiveFactor: 0.8, noLight: true });
  const glowMat = makeMat(`pugm-${type}`, scene, color, { noLight: true, alpha: 0.2, emissiveFactor: 1 });
  mats = { shapeMat, glowMat };
  _typeMats.set(type, mats);
  return mats;
}

export class PickupRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.meshes = new Map();
    this._time = 0;
  }

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
      entry.root.position.set(x, 8 + Math.sin(this._time * 2 + x * 0.01) * 3, z);
      entry.root.rotation.y = this._time * 1.5 + x * 0.1;
    }

    for (const [id, entry] of this.meshes) {
      if (!seen.has(id)) {
        this._dispose(entry);
        this.meshes.delete(id);
      }
    }
  }

  _create(p) {
    const B = window.BABYLON;
    const id = p.pickup_id;
    const type = p.pickup_type || 'health_pack';
    const mats = _getMats(type, this.scene);
    const root = new B.TransformNode(`pu-${id}`, this.scene);

    let mesh;
    if (type === 'health_pack') {
      // Red/green cross shape
      const h = B.MeshBuilder.CreateBox(`puh-${id}`, { width: 3, height: 8, depth: 3 }, this.scene);
      const v = B.MeshBuilder.CreateBox(`puv-${id}`, { width: 8, height: 3, depth: 3 }, this.scene);
      h.parent = root; v.parent = root;
      h.material = mats.shapeMat; v.material = mats.shapeMat;
      mesh = h; mesh._sibling = v;
    } else if (type === 'shield_bubble') {
      mesh = B.MeshBuilder.CreateSphere(`pum-${id}`, { diameter: 8, segments: 6 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
    } else if (type === 'speed_boost') {
      // Lightning bolt / arrow shape
      mesh = B.MeshBuilder.CreateCylinder(`pum-${id}`, { diameterTop: 0, diameterBottom: 7, height: 10, tessellation: 3 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
    } else {
      // damage_boost and fallback — diamond
      mesh = B.MeshBuilder.CreatePolyhedron(`pum-${id}`, { type: 1, size: 4 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
    }

    const glow = B.MeshBuilder.CreateDisc(`pug-${id}`, { radius: 12, tessellation: 8 }, this.scene);
    glow.rotation.x = Math.PI / 2;
    glow.position.y = -2;
    glow.parent = root;
    glow.material = mats.glowMat;

    return { root, mesh, glow };
  }

  _dispose(entry) {
    if (entry.mesh._sibling) entry.mesh._sibling.dispose();
    entry.mesh.dispose();
    entry.glow.dispose();
    entry.root.dispose();
    // Don't dispose shared materials
  }
}
