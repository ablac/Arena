'use strict';

/**
 * HUD overlay rendering — kill feed, round info, connection status.
 * Uses DOM elements overlaid on the canvas (not Babylon GUI).
 * @module renderer/hud
 */

export class HudRenderer {
  /**
   * @param {HTMLElement} roundEl - Round info element
   * @param {HTMLElement} killfeedEl - Kill feed element
   * @param {HTMLElement} statusEl - Connection status element
   */
  constructor(roundEl, killfeedEl, statusEl) {
    this.roundEl = roundEl;
    this.killfeedEl = killfeedEl;
    this.statusEl = statusEl;
    this.kills = [];
    this.maxKills = 10;
  }

  /**
   * Update HUD with arena state.
   * @param {Object} state - Arena state from spectator
   */
  updateState(state) {
    if (!state) return;
    this._updateRoundInfo(state);
    this._updateKillFeed(state.kill_feed || []);
  }

  /** @private */
  _updateRoundInfo(state) {
    const botsAlive = (state.bots || []).filter(b => b.is_alive).length;
    const totalBots = (state.bots || []).length;
    this.roundEl.innerHTML = `
      <div>Tick: <span style="color:var(--accent-blue)">${state.tick || 0}</span></div>
      <div>Bots: <span style="color:var(--accent-gold)">${botsAlive}</span> / ${totalBots}</div>
      <div>Zone: <span style="color:var(--accent-blue)">${Math.round(state.safe_zone?.radius || 0)}</span></div>
    `;
  }

  /** @private */
  _updateKillFeed(kills) {
    if (!kills || kills.length === 0) return;
    // Merge new kills (avoid duplicates by tick)
    const existingTicks = new Set(this.kills.map(k => `${k.killer}-${k.victim}-${k.tick}`));
    for (const kill of kills) {
      const key = `${kill.killer}-${kill.victim}-${kill.tick}`;
      if (!existingTicks.has(key)) {
        this.kills.unshift(kill);
      }
    }
    this.kills = this.kills.slice(0, this.maxKills);
    this._renderKillFeed();
  }

  /** @private */
  _renderKillFeed() {
    this.killfeedEl.innerHTML = this.kills.map(k =>
      `<div class="killfeed-entry">
        <span class="killer">${this._esc(k.killer)}</span>
        <span style="color:var(--text-muted)"> ${this._weaponIcon(k.weapon)} </span>
        <span class="victim">${this._esc(k.victim)}</span>
      </div>`
    ).join('');
  }

  /**
   * Update connection status display.
   * @param {string} status - 'connected' | 'disconnected' | 'connecting' | etc.
   */
  setStatus(status) {
    const connected = status === 'connected';
    this.statusEl.className = `ws-status${connected ? ' connected' : ''}`;
    this.statusEl.innerHTML = `<span class="dot"></span> ${connected ? 'Live' : status}`;
  }

  /** @private */
  _weaponIcon(weapon) {
    const icons = {
      sword: '\u2694', bow: '\uD83C\uDFF9', daggers: '\uD83D\uDDE1',
      shield: '\uD83D\uDEE1', spear: '\uD83D\uDD31', staff: '\uD83E\uDE84',
    };
    return icons[weapon] || '\u2620';
  }

  /** @private */
  _esc(str) {
    const d = document.createElement('div');
    d.textContent = str || '???';
    return d.innerHTML;
  }
}
