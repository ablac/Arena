'use strict';

/**
 * Minimap — small canvas overlay showing full arena zoomed out.
 * @module renderer/minimap
 */

import { isEnabled } from '../settings.js';

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
    // Framed card look (issue #184d): thin token-blue border, subtle outer
    // glow, slightly darker backdrop — matches the arena.css --accent-blue
    // (#47d7ff) token language. Static styling only; no animation here.
    this.canvas.style.cssText = `
      position:absolute; bottom:12px; right:12px; width:${MINIMAP_SIZE}px;
      height:${MINIMAP_SIZE}px; border:1px solid rgba(71,215,255,0.45);
      border-radius:10px; background:rgba(5,9,16,0.92); z-index:10;
      box-shadow:0 0 14px rgba(71,215,255,0.18), 0 6px 18px rgba(0,0,0,0.45);
      pointer-events:none;
    `;
    container.appendChild(this.canvas);
    this.canvas.style.display = 'none';
    this.ctx = this.canvas.getContext('2d');

    // Static layer: obstacle rects pre-rasterized to an offscreen canvas and
    // blitted per redraw, instead of re-filling every rect at 5 Hz. Rebuilt
    // only when the obstacle set changes (new keyframe array reference) or
    // the arena rescales.
    this._staticCanvas = document.createElement('canvas');
    this._staticCanvas.width = MINIMAP_SIZE;
    this._staticCanvas.height = MINIMAP_SIZE;
    this._staticCtx = this._staticCanvas.getContext('2d');
    this._staticDirty = true;
    this._lastObstacles = null;
  }

  /**
   * Redraw minimap from arena state.
   * @param {Object} state - Arena state
   */
  update(state) {
    if (!state) return;

    // Dynamic arena sizing: keyframes carry arena_size; rescale to match.
    const size = state.arena_size;
    if (size && size.length === 2 &&
        (size[0] !== this.arenaWidth || size[1] !== this.arenaHeight)) {
      this.arenaWidth = size[0];
      this.arenaHeight = size[1];
      this.scale = MINIMAP_SIZE / Math.max(this.arenaWidth, this.arenaHeight);
      this._lastObstacles = null;
      this._staticDirty = true;
    }
    // Obstacles arrive only on keyframe broadcasts — keep the last copy and
    // re-rasterize the static layer when the set changes (each keyframe
    // delivers a freshly parsed array, so a reference check catches it).
    if (state.obstacles && state.obstacles !== this._lastObstacles) {
      this._lastObstacles = state.obstacles;
      this._staticDirty = true;
    }

    const hasBots = state.bots && state.bots.some(b => b.is_alive);
    this.canvas.style.display = hasBots ? '' : 'none';
    if (!hasBots) return;
    const ctx = this.ctx;
    const s = this.scale;
    const now = performance.now();

    // Clear
    ctx.fillStyle = 'rgba(6, 10, 17, 0.95)';
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

      // Current zone boundary. The stroke alpha oscillates slowly (issue
      // #184d) so the shrinking ring reads live; sudden death tints it red
      // to match the 3D zone ring. update() runs at the throttled UI rate,
      // so this per-call isEnabled read is the standard per-frame-hook gate.
      ctx.beginPath();
      ctx.arc(
        state.safe_zone.center[0] * s,
        state.safe_zone.center[1] * s,
        state.safe_zone.radius * s,
        0, Math.PI * 2
      );
      ctx.fillStyle = 'rgba(0, 100, 200, 0.15)';
      ctx.fill();
      const zoneRGB = state.sudden_death ? '255, 70, 90' : '0, 180, 255';
      const zoneAlpha = isEnabled('gameplayZoneIndicators', 'minimapZonePulse')
        ? 0.38 + 0.22 * Math.sin(now / 420)
        : 0.5;
      ctx.strokeStyle = `rgba(${zoneRGB}, ${zoneAlpha.toFixed(3)})`;
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

    // Obstacles (pre-rasterized static layer, cached between keyframes)
    if (this._staticDirty) {
      this._renderStaticLayer();
      this._staticDirty = false;
    }
    ctx.drawImage(this._staticCanvas, 0, 0);

    // Pickups
    if (state.pickups) {
      for (const p of state.pickups) {
        ctx.fillStyle = this._pickupColor(p.pickup_type);
        ctx.fillRect(p.position[0] * s - 1, p.position[1] * s - 1, 2, 2);
      }
    }

    // Objective pings (issue #184d): pulsing rings around live flags and
    // bounty targets so objectives read at minimap scale. Same per-update
    // gate pattern as the zone pulse above.
    const pingsOn = isEnabled('objectiveIndicators', 'minimapPings');
    const pingR = 3.6 + Math.sin(now / 300) * 1.3;

    // CTF flags (team modes)
    if (state.flags) {
      const teamColors = ['#4a8dff', '#ff4a40', '#4de669', '#ffd933'];
      for (const f of state.flags) {
        ctx.fillStyle = teamColors[((f.team || 1) - 1) % teamColors.length];
        ctx.fillRect(f.position[0] * s - 2, f.position[1] * s - 2, 4, 4);
        ctx.strokeStyle = ctx.fillStyle;
        ctx.strokeRect(f.base_position[0] * s - 3, f.base_position[1] * s - 3, 6, 6);
        if (pingsOn) {
          ctx.beginPath();
          ctx.arc(f.position[0] * s, f.position[1] * s, pingR, 0, Math.PI * 2);
          ctx.lineWidth = 1;
          ctx.stroke();
        }
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
        if (pingsOn && bot.is_bounty_target) {
          // Gold bounty ping — matches the arena.css --accent-gold token.
          ctx.beginPath();
          ctx.arc(bot.position[0] * s, bot.position[1] * s, pingR, 0, Math.PI * 2);
          ctx.strokeStyle = 'rgba(255, 206, 84, 0.85)';
          ctx.lineWidth = 1;
          ctx.stroke();
        }
      }
    }

    // Border
    ctx.strokeStyle = 'rgba(71, 215, 255, 0.22)';
    ctx.lineWidth = 1;
    ctx.strokeRect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);
  }

  /**
   * @private Rasterize the round-static content (obstacle rects) onto the
   * offscreen static canvas. The rects use the same rgba fill on a
   * transparent layer, so the per-redraw drawImage blit composites over the
   * safe-zone tint exactly like the direct fillRect calls did. The border
   * stroke stays in update() because it draws on top of the dynamic bots.
   */
  _renderStaticLayer() {
    const ctx = this._staticCtx;
    ctx.clearRect(0, 0, MINIMAP_SIZE, MINIMAP_SIZE);
    if (!this._lastObstacles) return;
    const s = this.scale;
    ctx.fillStyle = 'rgba(40, 40, 55, 0.8)';
    for (const obs of this._lastObstacles) {
      ctx.fillRect(obs.x * s, obs.y * s, obs.width * s, obs.height * s);
    }
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
      overdrive_core: '#ff4fd8',
      grapple_charge: '#55dfff',
      relay_battery: '#ffb347',
    };
    return colors[type] || '#ffffff';
  }
}
