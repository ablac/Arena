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
    /** @type {BABYLON.Mesh|null} all pillar bodies merged (one draw call) */
    this._bodyMesh = null;
    /** @type {BABYLON.Mesh|null} all edge + base trims merged (one draw call) */
    this._trimMesh = null;
    this._mat = null;
    this._edgeMat = null;
    this._initMaterials();
  }

  /** @private Create shared materials. */
  _initMaterials() {
    const B = window.BABYLON;
    // Dark alloy body with restrained cool highlights.
    this._mat = new B.StandardMaterial('obsMat', this.scene);
    this._mat.diffuseColor = new B.Color3(0.07, 0.085, 0.11);
    this._mat.emissiveColor = new B.Color3(0.015, 0.03, 0.05);
    this._mat.specularColor = new B.Color3(0.12, 0.16, 0.24);
    this._mat.specularPower = 96;
    this._mat.backFaceCulling = false;
    this._mat.freeze();

    // Cleaner edge accent, less debug-neon.
    this._edgeMat = new B.StandardMaterial('obsEdgeMat', this.scene);
    this._edgeMat.diffuseColor = B.Color3.Black();
    this._edgeMat.emissiveColor = new B.Color3(0.08, 0.34, 0.62);
    this._edgeMat.disableLighting = true;
    this._edgeMat.alpha = 0.58;
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
    if (fp === this._lastFingerprint) return; // same layout — merged meshes stand

    // Obstacles changed — dispose the old merged meshes and rebuild from
    // scratch. This handles frozen world matrices and size changes between
    // rounds.
    this.clear();
    this._lastFingerprint = fp;
    if (!obstacles.length) {
      // Casters were just removed — the frozen shadow map still needs the
      // re-bake or it would keep showing the previous round's shadows.
      if (this._env && this._env.refreshShadows) this._env.refreshShadows();
      return;
    }

    // Build the 3 boxes per obstacle as before, but merge them into two
    // meshes — one per shared material. The layout is immutable for the
    // whole round (the fingerprint proves it), so 3 draw calls per obstacle
    // (150-450 on cave maps) collapse to 2 total. One merge call per
    // material group means no multi-materials are needed.
    const bodyBoxes = [];
    const trimBoxes = [];
    obstacles.forEach((obs, i) => {
      // Stone pillar
      const mesh = B.MeshBuilder.CreateBox(`obs-${i}`, {
        width: obs.width, height: PILLAR_HEIGHT, depth: obs.height
      }, this.scene);
      mesh.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT / 2, obs.y + obs.height / 2);
      bodyBoxes.push(mesh);

      // Glowing edge wireframe on top
      const edge = B.MeshBuilder.CreateBox(`obsEdge-${i}`, {
        width: obs.width + 1.5, height: 2, depth: obs.height + 1.5
      }, this.scene);
      edge.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT + 0.8, obs.y + obs.height / 2);
      trimBoxes.push(edge);

      // Bottom glow ring
      const base = B.MeshBuilder.CreateBox(`obsBase-${i}`, {
        width: obs.width + 1.5, height: 1.5, depth: obs.height + 1.5
      }, this.scene);
      base.position.set(obs.x + obs.width / 2, 0.75, obs.y + obs.height / 2);
      trimBoxes.push(base);
    });

    // MergeMeshes(meshes, disposeSource=true, allow32BitsIndices=true) —
    // sources are disposed inside the merge, so nothing leaks here.
    this._bodyMesh = B.Mesh.MergeMeshes(bodyBoxes, true, true);
    if (this._bodyMesh) {
      this._bodyMesh.name = 'obstacleBodies';
      this._bodyMesh.material = this._mat;
      this._bodyMesh.isPickable = false;
      this._bodyMesh.freezeWorldMatrix();
      // Only the opaque bodies cast — the trims are emissive decoration.
      if (this._env) this._env.addShadowCaster(this._bodyMesh);
    }
    this._trimMesh = B.Mesh.MergeMeshes(trimBoxes, true, true);
    if (this._trimMesh) {
      this._trimMesh.name = 'obstacleTrims';
      this._trimMesh.material = this._edgeMat;
      this._trimMesh.isPickable = false;
      this._trimMesh.freezeWorldMatrix();
    }

    // The environment's shadow map is frozen (RENDER_ONCE) — re-bake it now
    // that the casters changed.
    if (this._env && this._env.refreshShadows) this._env.refreshShadows();
  }

  /** Clear all obstacles (round reset). */
  clear() {
    if (this._bodyMesh) { this._bodyMesh.dispose(); this._bodyMesh = null; }
    if (this._trimMesh) { this._trimMesh.dispose(); this._trimMesh = null; }
  }
}
