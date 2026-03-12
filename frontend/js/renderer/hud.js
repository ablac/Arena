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
    this.killfeedEl.style.display = 'none';
  }

  /**
   * Update HUD with arena state.
   * @param {Object} state - Arena state from spectator
   */
  updateState(state) {
    if (!state) return;
    if (state.type === 'lobby_state') {
      this._updateLobbyInfo(state);
      return;
    }
    this._inLobby = false;
    this._updateRoundInfo(state);
    this._updateKillFeed(state.kill_feed || []);
  }

  /** @private */
  _updateLobbyInfo(state) {
    this._inLobby = true;
    this.killfeedEl.style.display = 'none';
    const players = state.players || [];
    let countdownHtml = '';
    if (state.countdown) {
      countdownHtml = `<div style="color:var(--accent-gold);font-size:1.2em;margin-top:4px">
        Round starts in: <span style="color:#fff">${state.countdown}s</span></div>`;
    } else {
      const needed = (state.bots_needed || 2) - (state.bots_connected || 0);
      countdownHtml = `<div style="color:var(--text-muted);margin-top:4px">
        Waiting for ${needed} more bot${needed !== 1 ? 's' : ''}...</div>`;
    }
    const playerList = players.map(p =>
      `<div style="padding:2px 0">
        <span style="color:${this._esc(p.avatar_color || '#fff')}">\u25CF</span>
        ${this._esc(p.name)} <span style="color:var(--text-muted)">[${this._esc(p.weapon)}]</span>
      </div>`
    ).join('');
    this.roundEl.innerHTML = `
      <div style="font-size:1.1em;color:var(--accent-blue);margin-bottom:4px">LOBBY</div>
      <div>Players: <span style="color:var(--accent-gold)">${state.bots_connected || 0}</span></div>
      ${countdownHtml}
      <div style="margin-top:6px;font-size:0.85em">${playerList}</div>
    `;
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
    if (!kills || kills.length === 0) {
      this.killfeedEl.style.display = this.kills.length === 0 ? 'none' : '';
      return;
    }
    this.killfeedEl.style.display = '';
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
