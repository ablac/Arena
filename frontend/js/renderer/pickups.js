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
    /**
     * Per-type disabled template parts. Every live pickup used to build its
     * own geometry (torus knots at radialSegments 64 are ~845 verts) and
     * draw as 1-3 separate calls; instances share the template's vertex
     * buffers, so a spawn allocates no geometry and all instances of a type
     * batch into one draw call. Templates also carry the HighlightLayer
     * registration once per type — the HL glow is per-type colored anyway.
     * @type {Map<string, {parts: BABYLON.Mesh[]}>}
     */
    this._templates = new Map();
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

  /**
   * Build (once per type) the disabled template meshes whose geometry and
   * material every live pickup of that type instances. Templates are
   * registered with the HighlightLayer here — instances of a registered
   * template glow with the type color, and the HL's RTT pass batches per
   * template instead of per pickup.
   */
  _getTemplate(type) {
    let tpl = this._templates.get(type);
    if (tpl) return tpl;
    const B = window.BABYLON;
    const mats = _getMats(type, this.scene);
    const parts = [];
    const part = (mesh) => { mesh.material = mats.shapeMat; parts.push(mesh); return mesh; };

    if (type === 'health_pack') {
      // Red/green cross shape
      part(B.MeshBuilder.CreateBox(`putpl-${type}-h`, { width: 3, height: 8, depth: 3 }, this.scene));
      part(B.MeshBuilder.CreateBox(`putpl-${type}-v`, { width: 8, height: 3, depth: 3 }, this.scene));
    } else if (type === 'shield_bubble') {
      part(B.MeshBuilder.CreateSphere(`putpl-${type}`, { diameter: 8, segments: 6 }, this.scene));
    } else if (type === 'speed_boost') {
      // Lightning bolt / arrow shape
      part(B.MeshBuilder.CreateCylinder(`putpl-${type}`, { diameterTop: 0, diameterBottom: 7, height: 10, tessellation: 3 }, this.scene));
    } else if (type === 'cooldown_shard') {
      part(B.MeshBuilder.CreateTorusKnot(`putpl-${type}`, { radius: 3.2, tube: 0.9, radialSegments: 64, tubularSegments: 12, p: 2, q: 3 }, this.scene))
        .rotation.x = Math.PI / 2;
    } else if (type === 'bounty_token') {
      part(B.MeshBuilder.CreateCylinder(`putpl-${type}`, { diameter: 7, height: 1.4, tessellation: 24 }, this.scene));
      part(B.MeshBuilder.CreateTorus(`putpl-${type}-ring`, { diameter: 9, thickness: 0.5, tessellation: 32 }, this.scene))
        .position.y = 0.2;
    } else if (type === 'hazard_key') {
      part(B.MeshBuilder.CreateTorusKnot(`putpl-${type}`, { radius: 3.1, tube: 0.85, radialSegments: 64, tubularSegments: 12, p: 3, q: 2 }, this.scene))
        .rotation.x = Math.PI / 2;
      part(B.MeshBuilder.CreateCylinder(`putpl-${type}-stem`, { diameter: 1.4, height: 7, tessellation: 12 }, this.scene))
        .position.y = -1.5;
    } else if (type === 'overdrive_core') {
      part(B.MeshBuilder.CreatePolyhedron(`putpl-${type}`, { type: 1, size: 4.8 }, this.scene));
      part(B.MeshBuilder.CreateTorus(`putpl-${type}-ring`, { diameter: 10, thickness: 0.65, tessellation: 36 }, this.scene))
        .rotation.x = Math.PI / 2;
    } else if (type === 'grapple_charge') {
      part(B.MeshBuilder.CreateTorusKnot(`putpl-${type}`, { radius: 2.7, tube: 0.7, radialSegments: 56, tubularSegments: 12, p: 2, q: 5 }, this.scene))
        .rotation.x = Math.PI / 2;
    } else if (type === 'relay_battery') {
      part(B.MeshBuilder.CreateCylinder(`putpl-${type}`, { diameter: 4.6, height: 8.5, tessellation: 16 }, this.scene));
      part(B.MeshBuilder.CreateSphere(`putpl-${type}-top`, { diameter: 3.2, segments: 10 }, this.scene))
        .position.y = 3.5;
      part(B.MeshBuilder.CreateSphere(`putpl-${type}-bottom`, { diameter: 3.2, segments: 10 }, this.scene))
        .position.y = -3.5;
    } else {
      // damage_boost and fallback — diamond
      part(B.MeshBuilder.CreatePolyhedron(`putpl-${type}`, { type: 1, size: 4 }, this.scene));
    }

    const hl = _getHighlightLayer(this.scene);
    const c = COLORS[type] || COLORS.health_pack;
    const hlColor = new B.Color3(c[0], c[1], c[2]);
    for (const m of parts) {
      m.setEnabled(false);
      m.isPickable = false;
      hl.addMesh(m, hlColor);
    }

    tpl = { parts };
    this._templates.set(type, tpl);
    return tpl;
  }

  _create(p) {
    const B = window.BABYLON;
    const id = p.pickup_id;
    const type = p.pickup_type || 'health_pack';
    const root = new B.TransformNode(`pu-${id}`, this.scene);

    const tpl = this._getTemplate(type);
    const instances = tpl.parts.map((partMesh, i) => {
      const inst = partMesh.createInstance(`pu-${id}-${i}`);
      inst.parent = root;
      inst.position.copyFrom(partMesh.position);
      inst.rotation.copyFrom(partMesh.rotation);
      return inst;
    });

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

    return { root, instances };
  }

  _dispose(entry) {
    // Templates stay registered on the HighlightLayer and keep their
    // geometry for the scene's lifetime; only the per-pickup instances and
    // transform go away.
    this.scene.stopAnimation(entry.root);
    for (const inst of entry.instances) inst.dispose();
    entry.root.dispose();
    // Don't dispose shared materials
  }
}
