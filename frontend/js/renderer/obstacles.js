'use strict';

/**
 * Obstacle rendering — stone pillars/walls with emissive edges.
 * @module renderer/obstacles
 */

import { makeMat } from './utils.js';

const PILLAR_HEIGHT = 30;

export class ObstacleRenderer {
  /** @param {BABYLON.Scene} scene @param {EnvironmentRenderer} [envRenderer] */
  constructor(scene, envRenderer) {
    this.scene = scene;
    this._env = envRenderer || null;
    /** @type {Map<number, {mesh: BABYLON.Mesh, edge: BABYLON.Mesh}>} */
    this.meshes = new Map();
    this._mat = null;
    this._edgeMat = null;
    this._initMaterials();
  }

  /** @private Create shared materials. */
  _initMaterials() {
    const B = window.BABYLON;
    // Dark metallic body with subtle blue emissive
    this._mat = new B.StandardMaterial('obsMat', this.scene);
    this._mat.diffuseColor = new B.Color3(0.08, 0.08, 0.12);
    this._mat.emissiveColor = new B.Color3(0.02, 0.05, 0.1);
    this._mat.specularColor = new B.Color3(0.15, 0.2, 0.35);
    this._mat.specularPower = 64;
    this._mat.backFaceCulling = false;
    this._mat.freeze();

    // Bright cyan edge glow
    this._edgeMat = new B.StandardMaterial('obsEdgeMat', this.scene);
    this._edgeMat.diffuseColor = B.Color3.Black();
    this._edgeMat.emissiveColor = new B.Color3(0.15, 0.5, 0.9);
    this._edgeMat.disableLighting = true;
    this._edgeMat.alpha = 0.7;
    this._edgeMat.freeze();
  }

  /**
   * Update obstacles from state.
   * @param {Array} obstacles - [{ x, y, width, height }]
   */
  update(obstacles) {
    if (!obstacles) return;
    const B = window.BABYLON;

    // Detect if obstacles changed (new round). Build a fingerprint from
    // the obstacle data to compare against last update.
    const fp = obstacles.map(o => `${o.x},${o.y},${o.width},${o.height}`).join('|');
    if (fp !== this._lastFingerprint) {
      // Obstacles changed — dispose all old meshes and rebuild from scratch.
      // This handles frozen world matrices and size changes between rounds.
      this.clear();
      this._lastFingerprint = fp;
    }

    const seen = new Set();

    obstacles.forEach((obs, i) => {
      seen.add(i);
      if (this.meshes.has(i)) {
        return; // Already created for this round's layout
      }

      // Stone pillar
      const mesh = B.MeshBuilder.CreateBox(`obs-${i}`, {
        width: obs.width, height: PILLAR_HEIGHT, depth: obs.height
      }, this.scene);
      mesh.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT / 2, obs.y + obs.height / 2);
      mesh.material = this._mat;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      if (this._env) this._env.addShadowCaster(mesh);

      // Glowing edge wireframe on top
      const edge = B.MeshBuilder.CreateBox(`obsEdge-${i}`, {
        width: obs.width + 1.5, height: 2, depth: obs.height + 1.5
      }, this.scene);
      edge.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT + 0.8, obs.y + obs.height / 2);
      edge.material = this._edgeMat;
      edge.isPickable = false;
      edge.freezeWorldMatrix();

      // Bottom glow ring
      const base = B.MeshBuilder.CreateBox(`obsBase-${i}`, {
        width: obs.width + 1.5, height: 1.5, depth: obs.height + 1.5
      }, this.scene);
      base.position.set(obs.x + obs.width / 2, 0.75, obs.y + obs.height / 2);
      base.material = this._edgeMat;
      base.isPickable = false;
      base.freezeWorldMatrix();

      this.meshes.set(i, { mesh, edge, base });
    });

    // Remove stale
    for (const [k, entry] of this.meshes) {
      if (!seen.has(k)) {
        entry.mesh.dispose();
        entry.edge.dispose();
        if (entry.base) entry.base.dispose();
        this.meshes.delete(k);
      }
    }
  }

  /** Clear all obstacles (round reset). */
  clear() {
    for (const [, entry] of this.meshes) {
      entry.mesh.dispose();
      entry.edge.dispose();
      if (entry.base) entry.base.dispose();
    }
    this.meshes.clear();
  }
}
