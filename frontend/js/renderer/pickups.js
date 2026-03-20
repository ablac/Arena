'use strict';

/**
 * Pickup rendering — floating, rotating collectibles with glow.
 * Materials shared by pickup type for performance.
 * @module renderer/pickups
 */

import { makeMat } from './utils.js';

let _highlightLayer = null;

function _getHighlightLayer(scene) {
  if (!_highlightLayer || _highlightLayer.isDisposed) {
    _highlightLayer = new window.BABYLON.HighlightLayer('pickupHL', scene, {
      blurHorizontalSize: 0.5,
      blurVerticalSize: 0.5,
    });
  }
  return _highlightLayer;
}

const COLORS = {
  health_pack:    [0.1, 1.0, 0.3],
  speed_boost:    [1.0, 1.0, 0.1],
  damage_boost:   [1.0, 0.25, 0.2],
  shield_bubble:  [0.25, 0.5, 1.0],
  gravity_well:   [0.5, 0.0, 1.0],
};

/** @type {Map<string, {shapeMat: BABYLON.StandardMaterial}>} */
const _typeMats = new Map();

function _getMats(type, scene) {
  let mats = _typeMats.get(type);
  if (mats && !mats.shapeMat.isDisposed) return mats;
  const B = window.BABYLON;
  const c = COLORS[type] || COLORS.health_pack;
  const color = new B.Color3(c[0], c[1], c[2]);
  const shapeMat = makeMat(`pumat-${type}`, scene, color, { emissiveFactor: 0.8, noLight: true });
  mats = { shapeMat };
  _typeMats.set(type, mats);
  return mats;
}

export class PickupRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} */
    this.meshes = new Map();
  }

  update(pickups) {
    const seen = new Set();

    for (const p of pickups) {
      seen.add(p.pickup_id);
      let entry = this.meshes.get(p.pickup_id);
      if (!entry) {
        entry = this._create(p);
        this.meshes.set(p.pickup_id, entry);
      }

      const x = p.position[0], z = p.position[1];
      entry.root.position.x = x;
      entry.root.position.z = z;
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

    const hl = _getHighlightLayer(this.scene);
    const c = COLORS[type] || COLORS.health_pack;
    const hlColor = new B.Color3(c[0], c[1], c[2]);
    hl.addMesh(mesh, hlColor);
    // For health_pack with sibling, add both
    if (mesh._sibling) hl.addMesh(mesh._sibling, hlColor);

    // Floating bob animation (Babylon Animation API — runs automatically)
    const bobAnim = new B.Animation('pickupBob', 'position.y', 30,
      B.Animation.ANIMATIONTYPE_FLOAT, B.Animation.ANIMATIONLOOPMODE_CYCLE);
    bobAnim.setKeys([
      { frame: 0, value: 5 },
      { frame: 15, value: 11 },
      { frame: 30, value: 5 },
    ]);
    this.scene.beginDirectAnimation(root, [bobAnim], 0, 30, true, 1.0 + Math.random() * 0.3);

    // Continuous rotation animation
    const rotAnim = new B.Animation('pickupRot', 'rotation.y', 30,
      B.Animation.ANIMATIONTYPE_FLOAT, B.Animation.ANIMATIONLOOPMODE_CYCLE);
    rotAnim.setKeys([
      { frame: 0, value: 0 },
      { frame: 30, value: Math.PI * 2 },
    ]);
    this.scene.beginDirectAnimation(root, [rotAnim], 0, 30, true, 0.8 + Math.random() * 0.4);

    return { root, mesh };
  }

  _dispose(entry) {
    const hl = _getHighlightLayer(this.scene);
    if (hl) {
      hl.removeMesh(entry.mesh);
      if (entry.mesh._sibling) hl.removeMesh(entry.mesh._sibling);
    }
    this.scene.stopAnimation(entry.root);
    if (entry.mesh._sibling) entry.mesh._sibling.dispose();
    entry.mesh.dispose();
    entry.root.dispose();
    // Don't dispose shared materials
  }
}
