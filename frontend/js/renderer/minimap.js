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
    this.canvas.style.display = 'none';
    this.ctx = this.canvas.getContext('2d');
  }

  /**
   * Redraw minimap from arena state.
   * @param {Object} state - Arena state
   */
  update(state) {
    if (!state) return;
    const hasBots = state.bots && state.bots.some(b => b.is_alive);
    this.canvas.style.display = hasBots ? '' : 'none';
    if (!hasBots) return;
    const ctx = this.ctx;
    const s = this.scale;

    // Clear
    ctx.fillStyle = 'rgba(10, 14, 23, 0.95)';
    ctx.fillRect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);

    // Safe zone
    if (state.safe_zone) {
      // Target zone (where the zone is shrinking to)
      if (state.safe_zone.target_center) {
        ctx.beginPath();
        ctx.arc(
          state.safe_zone.target_center[0] * s,
          state.safe_zone.target_center[1] * s,
          (state.safe_zone.target_radius || 75) * s,
          0, Math.PI * 2
        );
        ctx.strokeStyle = 'rgba(255, 255, 255, 0.2)';
        ctx.lineWidth = 1;
        ctx.setLineDash([2, 3]);
        ctx.stroke();
        ctx.setLineDash([]);
      }

      // Current zone boundary
      ctx.beginPath();
      ctx.arc(
        state.safe_zone.center[0] * s,
        state.safe_zone.center[1] * s,
        state.safe_zone.radius * s,
        0, Math.PI * 2
      );
      ctx.fillStyle = 'rgba(0, 100, 200, 0.15)';
      ctx.fill();
      ctx.strokeStyle = 'rgba(0, 180, 255, 0.5)';
      ctx.lineWidth = 1.5;
      ctx.stroke();

      // Danger tint outside zone
      ctx.save();
      ctx.beginPath();
      ctx.rect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);
      ctx.arc(
        state.safe_zone.center[0] * s,
        state.safe_zone.center[1] * s,
        state.safe_zone.radius * s,
        0, Math.PI * 2, true
      );
      ctx.fillStyle = 'rgba(200, 30, 10, 0.15)';
      ctx.fill();
      ctx.restore();
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
        ctx.fillStyle = this._pickupColor(p.pickup_type);
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
      health_pack: '#00ff4c',
      speed_boost: '#ffff00',
      damage_boost: '#ff3333',
      shield_bubble: '#3388ff',
      gravity_well: '#a855f7',
      cooldown_shard: '#22d3ee',
      bounty_token: '#f59e0b',
      hazard_key: '#b6ff4d',
    };
    return colors[type] || '#ffffff';
  }
}
