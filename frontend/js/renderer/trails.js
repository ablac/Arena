'use strict';

/**
 * Movement trail system — smooth fading ribbons behind moving bots.
 * Samples bot visual positions every render frame and builds a flat ribbon
 * mesh with vertex alpha fading from bot (opaque) to tail (transparent).
 * @module renderer/trails
 */

import { parseColor } from './utils.js';

const MAX_HISTORY = 20;        // number of position samples kept
const SAMPLE_INTERVAL = 0.03;  // seconds between samples (~33fps sampling)
const TRAIL_WIDTH = 6;         // half-width of ribbon perpendicular to direction
const TRAIL_Y = 0.4;           // height above ground

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /**
     * Per-bot trail state.
     * @type {Map<string, {history: Array<{x:number,z:number}>, mesh: BABYLON.Mesh|null, mat: BABYLON.StandardMaterial, timer: number}>}
     */
    this.trails = new Map();
  }

  /**
   * Called every render frame with the bot renderer's entries map.
   * Samples visual positions and rebuilds ribbon meshes.
   * @param {Map<string, Object>|null} botEntries
   * @param {number} dt - frame delta in seconds
   */
  render(botEntries, dt) {
    if (!botEntries) return;
    const B = window.BABYLON;
    const seen = new Set();

    for (const [botId, entry] of botEntries) {
      if (!entry.isAlive || !entry._interpReady) continue;
      seen.add(botId);

      const x = entry.root.position.x;
      const z = entry.root.position.z;

      let trail = this.trails.get(botId);
      if (!trail) {
        const color = entry.bodyMat ? entry.bodyMat.diffuseColor : new B.Color3(0.5, 0.5, 0.5);
        const mat = new B.StandardMaterial(`tmat-${botId}`, this.scene);
        mat.emissiveColor = color.clone();
        mat.diffuseColor = color.clone();
        mat.disableLighting = true;
        mat.backFaceCulling = false;
        mat.alpha = 1;
        trail = { history: [{ x, z }], mesh: null, mat, timer: 0 };
        this.trails.set(botId, trail);
      }

      // Sample position at fixed interval
      trail.timer += dt;
      if (trail.timer >= SAMPLE_INTERVAL) {
        trail.timer = 0;
        const last = trail.history[trail.history.length - 1];
        const dx = x - last.x;
        const dz = z - last.z;
        // Only add if bot actually moved
        if (dx * dx + dz * dz > 0.5) {
          trail.history.push({ x, z });
          if (trail.history.length > MAX_HISTORY) {
            trail.history.shift();
          }
        }
      }

      // Need at least 2 points for a ribbon
      if (trail.history.length < 2) continue;

      // Build ribbon paths: two parallel paths offset perpendicular to direction
      const left = [];
      const right = [];
      const hist = trail.history;

      for (let i = 0; i < hist.length; i++) {
        // Compute direction at this point
        let nx, nz;
        if (i < hist.length - 1) {
          nx = hist[i + 1].x - hist[i].x;
          nz = hist[i + 1].z - hist[i].z;
        } else {
          nx = hist[i].x - hist[i - 1].x;
          nz = hist[i].z - hist[i - 1].z;
        }
        const len = Math.sqrt(nx * nx + nz * nz) || 1;
        // Perpendicular direction (rotate 90°)
        const px = -nz / len;
        const pz = nx / len;

        // Alpha: 0 at tail (index 0), 1 at head (last index)
        const alpha = i / (hist.length - 1);
        const w = TRAIL_WIDTH * alpha; // taper from nothing to full width

        left.push(new B.Vector3(hist[i].x + px * w, TRAIL_Y, hist[i].z + pz * w));
        right.push(new B.Vector3(hist[i].x - px * w, TRAIL_Y, hist[i].z - pz * w));
      }

      // Dispose old mesh and create new ribbon
      if (trail.mesh) {
        trail.mesh.dispose();
        trail.mesh = null;
      }

      try {
        const ribbon = B.MeshBuilder.CreateRibbon(`trail-${botId}`, {
          pathArray: [left, right],
          updatable: false,
          sideOrientation: B.Mesh.DOUBLESIDE,
        }, this.scene);
        ribbon.material = trail.mat;
        ribbon.isPickable = false;
        ribbon.alwaysSelectAsActiveMesh = true;

        // Apply vertex alpha: fade from transparent (tail) to semi-opaque (head)
        const vertexCount = ribbon.getTotalVertices();
        const colors = new Float32Array(vertexCount * 4);
        const c = trail.mat.emissiveColor;
        const pointsPerSide = hist.length;

        for (let v = 0; v < vertexCount; v++) {
          // Each side has pointsPerSide vertices; vertex order is left[0..n-1], right[0..n-1]
          const idx = v % pointsPerSide;
          const alpha = idx / (pointsPerSide - 1);
          colors[v * 4 + 0] = c.r;
          colors[v * 4 + 1] = c.g;
          colors[v * 4 + 2] = c.b;
          colors[v * 4 + 3] = alpha * 0.3; // max 30% opacity at head
        }
        ribbon.setVerticesData(B.VertexBuffer.ColorKind, colors);
        ribbon.hasVertexAlpha = true;
        // Override material alpha since we use vertex alpha
        trail.mat.alpha = 1;
        trail.mat.useVertexAlpha = true;

        trail.mesh = ribbon;
      } catch {
        // CreateRibbon can fail with degenerate geometry — ignore
      }
    }

    // Cleanup trails for bots that are gone
    for (const [botId, trail] of this.trails) {
      if (!seen.has(botId)) {
        if (trail.mesh) trail.mesh.dispose();
        trail.mat.dispose();
        this.trails.delete(botId);
      }
    }
  }

  dispose() {
    for (const [, trail] of this.trails) {
      if (trail.mesh) trail.mesh.dispose();
      trail.mat.dispose();
    }
    this.trails.clear();
  }
}
