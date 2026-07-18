'use strict';

/**
 * Obstacle rendering — stone pillars/walls with emissive edges.
 * Per-map palettes and rooftop detailing (issue #182) are applied at the
 * round-boundary rebuild; the merged result stays at two draw calls.
 * Carved map-boundary rects (issue #186) are split out of the box build and
 * rendered by MapWallsRenderer as one smoothed contour wall (two more draw
 * calls: wall body + glow trim) when the server sends `mask_rects` and the
 * arenaAmbience.smoothMapWalls setting is on.
 * @module renderer/obstacles
 */

import { isEnabled } from '../settings.js';
import { MapWallsRenderer } from './map-walls.js?v=20260718d';

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
    /** @type {Array|null} carved map-boundary rects from the last keyframe */
    this._lastMaskRects = null;
    // Smooth boundary walls (issue #186) ride the same round-boundary
    // rebuild as the merged pillars, sharing this renderer's materials.
    this._mapWalls = new MapWallsRenderer(scene, envRenderer);
    this._initMaterials();
  }

  /** Wire the engine's GlowLayer through to the boundary-wall builder so
   *  the wall body can be excluded from glow (only its trim glows, #181). */
  setGlowLayer(glow) {
    this._mapWalls.setGlowLayer(glow);
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
   * @param {Array} [maskRects] - carved boundary rects (subset of obstacles),
   *   present only on keyframes from servers that itemize them (issue #186)
   */
  update(obstacles, maskRects) {
    if (!obstacles) return;
    const B = window.BABYLON;
    this._lastObstacles = obstacles;
    // Obstacles only arrive on keyframes and mask_rects rides the same
    // keyframes, so whenever obstacles are present the field is
    // authoritative — absent means square map or a pre-#186 server.
    this._lastMaskRects = maskRects && maskRects.length ? maskRects : null;

    // Smooth boundary walls (issue #186): when the server itemizes the
    // carved boundary rects, render them as one smoothed contour wall and
    // keep the per-cell pillar/trim build for the real obstacles only.
    // Setting off or no mask_rects → everything renders as boxes as before.
    const smoothWalls = !!this._lastMaskRects && isEnabled('arenaAmbience', 'smoothMapWalls');

    // Detect if obstacles changed (new round). Build a fingerprint from
    // the obstacle data to compare against last update. (Mask rects are a
    // verbatim subset of the obstacle list, so they can't change alone.)
    const fp = obstacles.map(o => `${o.x},${o.y},${o.width},${o.height}`).join('|') +
      (smoothWalls ? `#walls${this._lastMaskRects.length}` : '');
    if (fp === this._lastFingerprint) return; // same layout — merged meshes stand

    // Obstacles changed — dispose the old merged meshes and rebuild from
    // scratch. This handles frozen world matrices and size changes between
    // rounds. The palette retint and the environment's floor re-bake
    // (contact shadows) ride the same round-boundary trigger.
    this.clear();
    this._lastFingerprint = fp;
    this._applyPalette();

    // Boundary rects leave the pillar build, the rooftop detailing, and the
    // contact-shadow floor bake — they're map boundary, not architecture.
    // Matched by exact x/y/width/height key against the mask_rects list.
    let buildObstacles = obstacles;
    if (smoothWalls) {
      const maskKeys = new Set(this._lastMaskRects.map(o => `${o.x},${o.y},${o.width},${o.height}`));
      buildObstacles = obstacles.filter(o => !maskKeys.has(`${o.x},${o.y},${o.width},${o.height}`));
    }
    if (this._env && this._env.setRoundObstacles) this._env.setRoundObstacles(buildObstacles);
    // Built before the shadow re-bake below so the wall body is registered
    // as a caster in time; materials were just palette-tinted above.
    this._mapWalls.build(smoothWalls ? this._lastMaskRects : null, this._mat, this._edgeMat);
    if (!buildObstacles.length) {
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
    buildObstacles.forEach((obs, i) => {
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
    this.update(this._lastObstacles, this._lastMaskRects);
  }

  /**
   * Every live round-built mesh (merged pillar bodies/trims + smooth
   * boundary wall body/trim) for the intermission teardown sink (issue
   * #189). The director unfreezes and animates their transforms; the next
   * rebuild (or its fast-forward restore) disposes/normalizes them. The
   * boundary meshes are reached through the owned MapWallsRenderer's
   * private fields on purpose — this renderer drives its whole lifecycle.
   * @returns {BABYLON.Mesh[]}
   */
  collectTeardownMeshes() {
    return [
      this._bodyMesh,
      this._trimMesh,
      this._mapWalls._bodyMesh,
      this._mapWalls._trimMesh,
    ].filter(Boolean);
  }

  /** Shared palette-tinted materials for the intermission construction
   *  clones, so the transient rise matches the final build exactly. */
  getConstructionMaterials() {
    return { body: this._mat, trim: this._edgeMat };
  }

  /** Last keyframe layout (teardown dust needs somewhere to burst). */
  getLastLayout() {
    return {
      obstacles: this._lastObstacles || [],
      maskRects: this._lastMaskRects || [],
    };
  }

  /** Clear all obstacles (round reset). */
  clear() {
    if (this._bodyMesh) { this._bodyMesh.dispose(); this._bodyMesh = null; }
    if (this._trimMesh) { this._trimMesh.dispose(); this._trimMesh = null; }
    this._mapWalls.clear();
  }
}
