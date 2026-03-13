'use strict';

/**
 * Obstacle rendering — stone pillars/walls with emissive edges.
 * @module renderer/obstacles
 */

import { makeMat } from './utils.js';

const PILLAR_HEIGHT = 30;

export class ObstacleRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<number, {mesh: BABYLON.Mesh, edge: BABYLON.Mesh}>} */
    this.meshes = new Map();
    this._mat = null;
    this._edgeMat = null;
    this._initMaterials();
  }

  /** @private Create shared materials. */
  _initMaterials() {
    const B = window.BABYLON;
    this._mat = makeMat('obsMat', this.scene, new B.Color3(0.3, 0.28, 0.24), {
      emissiveFactor: 0.1, specular: new B.Color3(0.08, 0.08, 0.08), backFace: true
    });
    this._mat.freeze();
    this._edgeMat = makeMat('obsEdgeMat', this.scene, new B.Color3(0.4, 0.55, 0.7), {
      noLight: true, alpha: 0.4, emissiveFactor: 1
    });
    this._edgeMat.freeze();
  }

  /**
   * Update obstacles from state.
   * @param {Array} obstacles - [{ x, y, width, height }]
   */
  update(obstacles) {
    if (!obstacles) return;
    const B = window.BABYLON;
    const seen = new Set();

    obstacles.forEach((obs, i) => {
      seen.add(i);
      if (this.meshes.has(i)) {
        // Update position only
        const entry = this.meshes.get(i);
        entry.mesh.position.x = obs.x + obs.width / 2;
        entry.mesh.position.z = obs.y + obs.height / 2;
        entry.edge.position.x = obs.x + obs.width / 2;
        entry.edge.position.z = obs.y + obs.height / 2;
        return;
      }

      // Stone pillar
      const mesh = B.MeshBuilder.CreateBox(`obs-${i}`, {
        width: obs.width, height: PILLAR_HEIGHT, depth: obs.height
      }, this.scene);
      mesh.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT / 2, obs.y + obs.height / 2);
      mesh.material = this._mat;
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();

      // Glowing edge wireframe on top
      const edge = B.MeshBuilder.CreateBox(`obsEdge-${i}`, {
        width: obs.width + 1, height: 1.5, depth: obs.height + 1
      }, this.scene);
      edge.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT + 0.5, obs.y + obs.height / 2);
      edge.material = this._edgeMat;
      edge.isPickable = false;
      edge.freezeWorldMatrix();

      this.meshes.set(i, { mesh, edge });
    });

    // Remove stale
    for (const [k, entry] of this.meshes) {
      if (!seen.has(k)) {
        entry.mesh.dispose();
        entry.edge.dispose();
        this.meshes.delete(k);
      }
    }
  }

  /** Clear all obstacles (round reset). */
  clear() {
    for (const [, entry] of this.meshes) {
      entry.mesh.dispose();
      entry.edge.dispose();
    }
    this.meshes.clear();
  }
}
