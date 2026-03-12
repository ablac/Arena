'use strict';

/**
 * Minimap — small canvas overlay showing full arena zoomed out.
 * @module renderer/minimap
 */

const MINIMAP_SIZE = 150;
const MINIMAP_PADDING = 4;

export class Minimap {
  /**
   * @param {HTMLElement} container - Container element for the minimap
   * @param {number} arenaWidth
   * @param {number} arenaHeight
   */
  constructor(container, arenaWidth, arenaHeight) {
    this.arenaWidth = arenaWidth;
    this.arenaHeight = arenaHeight;
    this.scale = MINIMAP_SIZE / Math.max(arenaWidth, arenaHeight);

    this.canvas = document.createElement('canvas');
    this.canvas.width = MINIMAP_SIZE;
    this.canvas.height = MINIMAP_SIZE;
    this.canvas.style.cssText = `
      position:absolute; bottom:12px; right:12px; width:${MINIMAP_SIZE}px;
      height:${MINIMAP_SIZE}px; border:1px solid #1e293b;
      border-radius:8px; background:rgba(10,14,23,0.9); z-index:10;
    `;
    container.appendChild(this.canvas);
    this.ctx = this.canvas.getContext('2d');
  }

  /**
   * Redraw minimap from arena state.
   * @param {Object} state - Arena state
   */
  update(state) {
    if (!state) return;
    const ctx = this.ctx;
    const s = this.scale;

    // Clear
    ctx.fillStyle = 'rgba(10, 14, 23, 0.95)';
    ctx.fillRect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);

    // Safe zone
    if (state.safe_zone) {
      ctx.beginPath();
      ctx.arc(
        state.safe_zone.center[0] * s,
        state.safe_zone.center[1] * s,
        state.safe_zone.radius * s,
        0, Math.PI * 2
      );
      ctx.fillStyle = 'rgba(0, 100, 200, 0.15)';
      ctx.fill();
      ctx.strokeStyle = 'rgba(0, 180, 255, 0.4)';
      ctx.lineWidth = 1;
      ctx.stroke();
    }

    // Obstacles
    ctx.fillStyle = 'rgba(40, 40, 55, 0.8)';
    if (state.obstacles) {
      for (const obs of state.obstacles) {
        ctx.fillRect(obs.x * s, obs.y * s, obs.width * s, obs.height * s);
      }
    }

    // Pickups
    if (state.pickups) {
      for (const p of state.pickups) {
        ctx.fillStyle = this._pickupColor(p.type);
        ctx.fillRect(p.position[0] * s - 1, p.position[1] * s - 1, 2, 2);
      }
    }

    // Bots
    if (state.bots) {
      for (const bot of state.bots) {
        if (!bot.is_alive) continue;
        ctx.fillStyle = bot.avatar_color || '#ffffff';
        ctx.beginPath();
        ctx.arc(bot.position[0] * s, bot.position[1] * s, 2, 0, Math.PI * 2);
        ctx.fill();
      }
    }

    // Border
    ctx.strokeStyle = '#1e293b';
    ctx.lineWidth = 1;
    ctx.strokeRect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);
  }

  /** @private */
  _pickupColor(type) {
    const colors = {
      health: '#00ff4c', speed: '#ffff00', damage: '#ff3333', shield: '#3388ff',
    };
    return colors[type] || '#ffffff';
  }
}
