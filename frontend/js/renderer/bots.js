'use strict';

/**
 * Bot rendering — colored discs with health bars and weapon indicators.
 * Sprite-ready: swap disc meshes for sprite meshes later.
 * @module renderer/bots
 */

const BOT_RADIUS = 8;
const HEALTH_BAR_WIDTH = 16;
const HEALTH_BAR_HEIGHT = 2;
const HEALTH_BAR_OFFSET_Y = 2;

/** Weapon indicator shapes. */
const WEAPON_COLORS = {
  sword: [0.8, 0.8, 0.8],
  bow: [0.2, 0.8, 0.2],
  daggers: [0.9, 0.5, 0.1],
  shield: [0.3, 0.5, 0.9],
  spear: [0.7, 0.3, 0.7],
  staff: [0.5, 0.2, 0.9],
};

export class BotRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, Object>} bot_id -> { mesh, healthBar, weaponDot } */
    this.meshes = new Map();
  }

  /**
   * Update bot visuals from arena state.
   * @param {Array} bots - Bot array from spectator data
   */
  update(bots) {
    const BABYLON = window.BABYLON;
    const seen = new Set();

    for (const bot of bots) {
      seen.add(bot.bot_id);
      let entry = this.meshes.get(bot.bot_id);

      if (!entry) {
        entry = this._createBot(bot);
        this.meshes.set(bot.bot_id, entry);
      }

      // Position (x on X-axis, y on Z-axis, Y is up in Babylon)
      entry.mesh.position.x = bot.position[0];
      entry.mesh.position.z = bot.position[1];
      entry.mesh.isVisible = bot.is_alive;

      // Health bar
      const hpRatio = bot.hp / bot.max_hp;
      entry.healthBar.scaling.x = Math.max(0.01, hpRatio);
      entry.healthBar.position.x = bot.position[0] - (HEALTH_BAR_WIDTH * (1 - hpRatio)) / 2;
      entry.healthBar.position.z = bot.position[1] - BOT_RADIUS - HEALTH_BAR_OFFSET_Y;
      entry.healthBar.isVisible = bot.is_alive;
      this._setHealthColor(entry.healthMat, hpRatio);

      // Weapon indicator
      entry.weaponDot.position.x = bot.position[0] + BOT_RADIUS + 3;
      entry.weaponDot.position.z = bot.position[1];
      entry.weaponDot.isVisible = bot.is_alive;
    }

    // Remove bots that are gone
    for (const [id, entry] of this.meshes) {
      if (!seen.has(id)) {
        entry.mesh.dispose();
        entry.healthBar.dispose();
        entry.healthMat.dispose();
        entry.weaponDot.dispose();
        entry.weaponMat.dispose();
        entry.mat.dispose();
        this.meshes.delete(id);
      }
    }
  }

  /** @private Create meshes for a bot. */
  _createBot(bot) {
    const BABYLON = window.BABYLON;
    const id = bot.bot_id;

    // Main disc (sprite-ready placeholder)
    const mesh = BABYLON.MeshBuilder.CreateDisc(`bot-${id}`, { radius: BOT_RADIUS }, this.scene);
    mesh.rotation.x = Math.PI / 2; // Flat on ground
    mesh.position.y = 1;
    const mat = new BABYLON.StandardMaterial(`mat-bot-${id}`, this.scene);
    const color = this._parseColor(bot.avatar_color);
    mat.diffuseColor = color;
    mat.emissiveColor = color;
    mat.disableLighting = true;
    mat.backFaceCulling = false;
    mesh.material = mat;

    // Health bar
    const healthBar = BABYLON.MeshBuilder.CreatePlane(`hp-${id}`, {
      width: HEALTH_BAR_WIDTH, height: HEALTH_BAR_HEIGHT
    }, this.scene);
    healthBar.rotation.x = Math.PI / 2;
    healthBar.position.y = 1.5;
    const healthMat = new BABYLON.StandardMaterial(`mat-hp-${id}`, this.scene);
    healthMat.diffuseColor = new BABYLON.Color3(0, 1, 0);
    healthMat.emissiveColor = new BABYLON.Color3(0, 1, 0);
    healthMat.disableLighting = true;
    healthMat.backFaceCulling = false;
    healthBar.material = healthMat;

    // Weapon indicator dot
    const weaponDot = BABYLON.MeshBuilder.CreateDisc(`wpn-${id}`, { radius: 3 }, this.scene);
    weaponDot.rotation.x = Math.PI / 2;
    weaponDot.position.y = 1;
    const weaponMat = new BABYLON.StandardMaterial(`mat-wpn-${id}`, this.scene);
    const wc = WEAPON_COLORS[bot.weapon] || [1, 1, 1];
    weaponMat.diffuseColor = new BABYLON.Color3(wc[0], wc[1], wc[2]);
    weaponMat.emissiveColor = new BABYLON.Color3(wc[0], wc[1], wc[2]);
    weaponMat.disableLighting = true;
    weaponMat.backFaceCulling = false;
    weaponDot.material = weaponMat;

    return { mesh, mat, healthBar, healthMat, weaponDot, weaponMat };
  }

  /** @private Parse hex color to BABYLON.Color3. */
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

  /** @private Set health bar color based on ratio. */
  _setHealthColor(mat, ratio) {
    const BABYLON = window.BABYLON;
    if (ratio > 0.6) {
      mat.diffuseColor = new BABYLON.Color3(0, 1, 0);
    } else if (ratio > 0.3) {
      mat.diffuseColor = new BABYLON.Color3(1, 1, 0);
    } else {
      mat.diffuseColor = new BABYLON.Color3(1, 0, 0);
    }
    mat.emissiveColor = mat.diffuseColor.clone();
  }
}
