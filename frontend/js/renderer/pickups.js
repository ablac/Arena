'use strict';

/**
 * Pickup rendering — floating, rotating collectibles with glow.
 * Materials shared by pickup type for performance.
 * @module renderer/pickups
 */

import { makeMat } from './utils.js';

let _highlightLayer = null;
let _highlightLayerScene = null;

function _getHighlightLayer(scene) {
  // HighlightLayer has no `isDisposed` member (the old check was always
  // undefined). Track the owning scene so a layer from a disposed/re-created
  // scene is never handed out.
  if (!_highlightLayer || _highlightLayerScene !== scene) {
    _highlightLayer = new window.BABYLON.HighlightLayer('pickupHL', scene, {
      blurHorizontalSize: 0.5,
      blurVerticalSize: 0.5,
    });
    _highlightLayerScene = scene;
  }
  return _highlightLayer;
}

const COLORS = {
  health_pack:    [0.1, 1.0, 0.3],
  speed_boost:    [1.0, 1.0, 0.1],
  damage_boost:   [1.0, 0.25, 0.2],
  shield_bubble:  [0.25, 0.5, 1.0],
  gravity_well:   [0.5, 0.0, 1.0],
  cooldown_shard: [0.2, 0.95, 1.0],
  bounty_token:   [1.0, 0.72, 0.18],
  hazard_key:     [0.72, 1.0, 0.32],
  overdrive_core: [1.0, 0.25, 0.85],
  grapple_charge: [0.30, 0.90, 1.0],
  relay_battery:  [1.0, 0.82, 0.36],
};

/** @type {Map<string, {shapeMat: BABYLON.StandardMaterial}>} */
const _typeMats = new Map();

function _getMats(type, scene) {
  let mats = _typeMats.get(type);
  // Materials have no `isDisposed` member; validate against the scene.
  if (mats && mats.shapeMat.getScene() === scene) return mats;
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
    } else if (type === 'cooldown_shard') {
      mesh = B.MeshBuilder.CreateTorusKnot(`pum-${id}`, { radius: 3.2, tube: 0.9, radialSegments: 64, tubularSegments: 12, p: 2, q: 3 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      mesh.rotation.x = Math.PI / 2;
    } else if (type === 'bounty_token') {
      mesh = B.MeshBuilder.CreateCylinder(`pum-${id}`, { diameter: 7, height: 1.4, tessellation: 24 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      const ring = B.MeshBuilder.CreateTorus(`pur-${id}`, { diameter: 9, thickness: 0.5, tessellation: 32 }, this.scene);
      ring.parent = root;
      ring.material = mats.shapeMat;
      ring.position.y = 0.2;
      mesh._sibling = ring;
    } else if (type === 'hazard_key') {
      mesh = B.MeshBuilder.CreateTorusKnot(`pum-${id}`, { radius: 3.1, tube: 0.85, radialSegments: 64, tubularSegments: 12, p: 3, q: 2 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      mesh.rotation.x = Math.PI / 2;
      const stem = B.MeshBuilder.CreateCylinder(`pus-${id}`, { diameter: 1.4, height: 7, tessellation: 12 }, this.scene);
      stem.parent = root;
      stem.material = mats.shapeMat;
      stem.position.y = -1.5;
      mesh._sibling = stem;
    } else if (type === 'overdrive_core') {
      mesh = B.MeshBuilder.CreatePolyhedron(`pum-${id}`, { type: 1, size: 4.8 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      const ring = B.MeshBuilder.CreateTorus(`pur-${id}`, { diameter: 10, thickness: 0.65, tessellation: 36 }, this.scene);
      ring.parent = root;
      ring.material = mats.shapeMat;
      ring.rotation.x = Math.PI / 2;
      mesh._sibling = ring;
    } else if (type === 'grapple_charge') {
      mesh = B.MeshBuilder.CreateTorusKnot(`pum-${id}`, { radius: 2.7, tube: 0.7, radialSegments: 56, tubularSegments: 12, p: 2, q: 5 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      mesh.rotation.x = Math.PI / 2;
    } else if (type === 'relay_battery') {
      mesh = B.MeshBuilder.CreateCylinder(`pum-${id}`, { diameter: 4.6, height: 8.5, tessellation: 16 }, this.scene);
      mesh.parent = root;
      mesh.material = mats.shapeMat;
      const capTop = B.MeshBuilder.CreateSphere(`put-${id}`, { diameter: 3.2, segments: 10 }, this.scene);
      capTop.parent = root;
      capTop.material = mats.shapeMat;
      capTop.position.y = 3.5;
      const capBottom = B.MeshBuilder.CreateSphere(`pub-${id}`, { diameter: 3.2, segments: 10 }, this.scene);
      capBottom.parent = root;
      capBottom.material = mats.shapeMat;
      capBottom.position.y = -3.5;
      mesh._sibling = capTop;
      mesh._extraSibling = capBottom;
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
    if (mesh._extraSibling) hl.addMesh(mesh._extraSibling, hlColor);

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
      if (entry.mesh._extraSibling) hl.removeMesh(entry.mesh._extraSibling);
    }
    this.scene.stopAnimation(entry.root);
    if (entry.mesh._sibling) entry.mesh._sibling.dispose();
    if (entry.mesh._extraSibling) entry.mesh._extraSibling.dispose();
    entry.mesh.dispose();
    entry.root.dispose();
    // Don't dispose shared materials
  }
}
