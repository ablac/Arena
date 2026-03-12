'use strict';

/**
 * Movement trail system — translucent fading discs behind moving bots.
 * @module renderer/trails
 */

const MAX_TRAIL_POINTS = 8;
const TRAIL_LIFETIME_MS = 500;
const TRAIL_RADIUS = 18;
const MIN_MOVE_DIST = 3; // Minimum movement to spawn a trail point

export class TrailRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Array<{mesh: BABYLON.Mesh, mat: BABYLON.StandardMaterial, created: number}>>} */
    this.trails = new Map();
    /** @type {Map<string, number[]>} bot_id -> last [x, z] */
    this.lastPositions = new Map();
    /** Shared template disc — cloned for performance */
    this._templateDisc = null;
  }

  /**
   * Update trails for all bots.
   * @param {Array} bots - Bot array from arena state
   */
  update(bots) {
    const now = Date.now();
    const aliveBots = new Set();

    for (const bot of bots) {
      if (!bot.is_alive) continue;
      aliveBots.add(bot.bot_id);

      const x = bot.position[0];
      const z = bot.position[1];
      const lastPos = this.lastPositions.get(bot.bot_id);

      if (lastPos) {
        const dx = x - lastPos[0];
        const dz = z - lastPos[1];
        const dist = Math.sqrt(dx * dx + dz * dz);

        if (dist > MIN_MOVE_DIST) {
          this._addTrailPoint(bot.bot_id, lastPos[0], lastPos[1], bot.avatar_color, now);
        }
      }

      this.lastPositions.set(bot.bot_id, [x, z]);
    }

    // Cleanup expired trail points and dead bot trails
    for (const [botId, points] of this.trails) {
      if (!aliveBots.has(botId)) {
        // Bot died — dispose all trail points
        for (const pt of points) {
          pt.mesh.dispose();
          pt.mat.dispose();
        }
        this.trails.delete(botId);
        this.lastPositions.delete(botId);
        continue;
      }

      // Remove expired points and update alpha for remaining
      for (let i = points.length - 1; i >= 0; i--) {
        const age = now - points[i].created;
        if (age > TRAIL_LIFETIME_MS) {
          points[i].mesh.dispose();
          points[i].mat.dispose();
          points.splice(i, 1);
        } else {
          // Fade out based on age
          const t = 1 - (age / TRAIL_LIFETIME_MS);
          points[i].mat.alpha = t * 0.25;
          const scale = 0.4 + t * 0.6;
          points[i].mesh.scaling.x = scale;
          points[i].mesh.scaling.z = scale;
        }
      }
    }
  }

  /** @private */
  _addTrailPoint(botId, x, z, hexColor, now) {
    const BABYLON = window.BABYLON;
    let points = this.trails.get(botId);
    if (!points) {
      points = [];
      this.trails.set(botId, points);
    }

    // Evict oldest if at capacity
    if (points.length >= MAX_TRAIL_POINTS) {
      const oldest = points.shift();
      oldest.mesh.dispose();
      oldest.mat.dispose();
    }

    const mesh = BABYLON.MeshBuilder.CreateDisc(`trail-${botId}-${now}`, {
      radius: TRAIL_RADIUS, tessellation: 12
    }, this.scene);
    mesh.rotation.x = Math.PI / 2;
    mesh.position.set(x, 0.3, z);

    const mat = new BABYLON.StandardMaterial(`mat-trail-${botId}-${now}`, this.scene);
    const color = this._parseColor(hexColor);
    mat.emissiveColor = color;
    mat.diffuseColor = color;
    mat.disableLighting = true;
    mat.alpha = 0.25;
    mat.backFaceCulling = false;
    mesh.material = mat;

    points.push({ mesh, mat, created: now });
  }

  /** @private */
  _parseColor(hex) {
    const BABYLON = window.BABYLON;
    if (!hex || typeof hex !== 'string' || hex.length < 7) {
      return new BABYLON.Color3(0.5, 0.5, 0.5);
    }
    const r = parseInt(hex.slice(1, 3), 16) / 255;
    const g = parseInt(hex.slice(3, 5), 16) / 255;
    const b = parseInt(hex.slice(5, 7), 16) / 255;
    if (isNaN(r) || isNaN(g) || isNaN(b)) {
      return new BABYLON.Color3(0.5, 0.5, 0.5);
    }
    return new BABYLON.Color3(r, g, b);
  }

  /** Dispose all trail meshes. */
  dispose() {
    for (const [, points] of this.trails) {
      for (const pt of points) {
        pt.mesh.dispose();
        pt.mat.dispose();
      }
    }
    this.trails.clear();
    this.lastPositions.clear();
  }
}
