'use strict';

/**
 * Obstacle rendering — stone pillars/walls with emissive edges.
 * Per-map palettes and rooftop detailing (issue #182) are applied at the
 * round-boundary rebuild; the merged result stays at two draw calls.
 * @module renderer/obstacles
 */

import { isEnabled } from '../settings.js';

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
    this._lastObstacles = null;
    this._initMaterials();
  }

  /** @private Create shared materials (default-palette hues; retinted per map). */
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

  /** @private Retint the shared materials from the environment's map palette. */
  _applyPalette() {
    const palette = this._env && this._env.getPalette ? this._env.getPalette() : null;
    if (!palette) return;
    this._mat.unfreeze();
    this._mat.diffuseColor.set(...palette.obstacleBody.diffuse);
    this._mat.emissiveColor.set(...palette.obstacleBody.emissive);
    this._mat.freeze();
    this._edgeMat.unfreeze();
    this._edgeMat.emissiveColor.set(...palette.obstacleTrim);
    this._edgeMat.freeze();
  }

  /**
   * Update obstacles from state.
   * @param {Array} obstacles - [{ x, y, width, height }]
   */
  update(obstacles) {
    if (!obstacles) return;
    const B = window.BABYLON;
    this._lastObstacles = obstacles;

    // Detect if obstacles changed (new round). Build a fingerprint from
    // the obstacle data to compare against last update.
    const fp = obstacles.map(o => `${o.x},${o.y},${o.width},${o.height}`).join('|');
    if (fp === this._lastFingerprint) return; // same layout — merged meshes stand

    // Obstacles changed — dispose the old merged meshes and rebuild from
    // scratch. This handles frozen world matrices and size changes between
    // rounds. The palette retint and the environment's floor re-bake
    // (contact shadows) ride the same round-boundary trigger.
    this.clear();
    this._lastFingerprint = fp;
    this._applyPalette();
    if (this._env && this._env.setRoundObstacles) this._env.setRoundObstacles(obstacles);
    if (!obstacles.length) {
      // Casters were just removed — the frozen shadow map still needs the
      // re-bake or it would keep showing the previous round's shadows.
      if (this._env && this._env.refreshShadows) this._env.refreshShadows();
      return;
    }

    // Build the boxes per obstacle as before, but merge them into two
    // meshes — one per shared material. The layout is immutable for the
    // whole round (the fingerprint proves it), so 3-5 draw calls per
    // obstacle collapse to 2 total. One merge call per material group means
    // no multi-materials are needed.
    const detailing = isEnabled('arenaAmbience', 'obstacleDetailing');
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

      // Rooftop detailing (issue #182c): a raised inset panel per pillar,
      // with a small glowing stud on every third one, so structures read as
      // architecture instead of extruded rectangles. Variation is derived
      // purely from the obstacle index (golden-ratio hash) — rebuilding the
      // same layout always produces the same roofs, no Math.random drift.
      // Panels share the body material and studs the trim material, so both
      // merge into the existing two draw calls.
      if (detailing) {
        const hash = (i * 0.61803398875) % 1;
        const inset = 0.52 + hash * 0.3;
        const raise = 0.9 + (i % 3) * 0.5;
        const panel = B.MeshBuilder.CreateBox(`obsTop-${i}`, {
          width: Math.max(2, obs.width * inset),
          height: raise,
          depth: Math.max(2, obs.height * inset),
        }, this.scene);
        panel.position.set(obs.x + obs.width / 2, PILLAR_HEIGHT + raise / 2, obs.y + obs.height / 2);
        bodyBoxes.push(panel);

        if (i % 3 === 0) {
          const stud = B.MeshBuilder.CreateBox(`obsStud-${i}`, {
            width: Math.max(1.2, obs.width * 0.18),
            height: 0.9,
            depth: Math.max(1.2, obs.height * 0.18),
          }, this.scene);
          stud.position.set(
            obs.x + obs.width / 2 + ((i % 4) < 2 ? -1 : 1) * obs.width * inset * 0.22,
            PILLAR_HEIGHT + raise + 0.45,
            obs.y + obs.height / 2 + (i % 2 ? -1 : 1) * obs.height * inset * 0.22,
          );
          trimBoxes.push(stud);
        }
      }
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

  /**
   * Force a rebuild of the current layout with fresh settings/palette state.
   * Called from the engine's settings-change hook when a world-identity
   * toggle flips mid-round; a no-op until the first keyframe delivers a
   * layout.
   */
  refresh() {
    if (!this._lastObstacles) return;
    this._lastFingerprint = null;
    this.update(this._lastObstacles);
  }

  /** Clear all obstacles (round reset). */
  clear() {
    if (this._bodyMesh) { this._bodyMesh.dispose(); this._bodyMesh = null; }
    if (this._trimMesh) { this._trimMesh.dispose(); this._trimMesh = null; }
  }
}
