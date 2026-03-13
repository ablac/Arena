'use strict';

/**
 * Movement trail system — translucent fading discs behind moving bots.
 * Uses a shared template disc + per-bot material for performance.
 * @module renderer/trails
 */

import { parseColor } from './utils.js';

const MAX_TRAIL_POINTS = 3;
const TRAIL_LIFETIME_MS = 250;
const TRAIL_RADIUS = 12;
const MIN_MOVE_DIST = 6;

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, {mat: BABYLON.StandardMaterial, points: Array}>} */
    this.trails = new Map();
    /** @type {Map<string, number[]>} */
    this.lastPositions = new Map();
    this._template = null;
  }

  /** @private Get or create the template disc for cloning. */
  _getTemplate() {
    if (!this._template || this._template.isDisposed()) {
      this._template = window.BABYLON.MeshBuilder.CreateDisc('trail-tpl', {
        radius: TRAIL_RADIUS, tessellation: 8
      }, this.scene);
      this._template.rotation.x = Math.PI / 2;
      this._template.setEnabled(false);
    }
    return this._template;
  }

  update(bots) {
    const now = Date.now();
    const aliveBots = new Set();

    for (const bot of bots) {
      if (!bot.is_alive) continue;
      aliveBots.add(bot.bot_id);

      const x = bot.position[0], z = bot.position[1];
      const lastPos = this.lastPositions.get(bot.bot_id);

      if (lastPos) {
        const dx = x - lastPos[0], dz = z - lastPos[1];
        if (dx * dx + dz * dz > MIN_MOVE_DIST * MIN_MOVE_DIST) {
          this._addTrailPoint(bot.bot_id, lastPos[0], lastPos[1], bot.avatar_color, now);
        }
      }
      this.lastPositions.set(bot.bot_id, [x, z]);
    }

    // Cleanup
    for (const [botId, trail] of this.trails) {
      if (!aliveBots.has(botId)) {
        for (const pt of trail.points) pt.mesh.dispose();
        trail.mat.dispose();
        this.trails.delete(botId);
        this.lastPositions.delete(botId);
        continue;
      }
      for (let i = trail.points.length - 1; i >= 0; i--) {
        const age = now - trail.points[i].created;
        if (age > TRAIL_LIFETIME_MS) {
          trail.points[i].mesh.dispose();
          trail.points.splice(i, 1);
        } else {
          const t = 1 - age / TRAIL_LIFETIME_MS;
          trail.points[i].mesh.visibility = t * 0.25;
          const scale = 0.4 + t * 0.6;
          trail.points[i].mesh.scaling.x = scale;
          trail.points[i].mesh.scaling.z = scale;
        }
      }
    }
  }

  /** @private */
  _addTrailPoint(botId, x, z, hexColor, now) {
    let trail = this.trails.get(botId);
    if (!trail) {
      const B = window.BABYLON;
      const color = parseColor(hexColor);
      const mat = new B.StandardMaterial(`tmat-${botId}`, this.scene);
      mat.emissiveColor = color;
      mat.diffuseColor = color;
      mat.disableLighting = true;
      mat.backFaceCulling = false;
      trail = { mat, points: [] };
      this.trails.set(botId, trail);
    }

    if (trail.points.length >= MAX_TRAIL_POINTS) {
      trail.points.shift().mesh.dispose();
    }

    const mesh = this._getTemplate().clone(`tr-${botId}-${now}`);
    mesh.position.set(x, 0.3, z);
    mesh.material = trail.mat;
    mesh.setEnabled(true);
    mesh.visibility = 0.25;

    trail.points.push({ mesh, created: now });
  }

  dispose() {
    for (const [, trail] of this.trails) {
      for (const pt of trail.points) pt.mesh.dispose();
      trail.mat.dispose();
    }
    this.trails.clear();
    this.lastPositions.clear();
    if (this._template) this._template.dispose();
  }
}
