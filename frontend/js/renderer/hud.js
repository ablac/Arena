'use strict';

/**
 * HUD overlay rendering — kill feed, round info, player roster, connection status.
 * Kill feed and player list render into tab panels above the arena.
 * Round info stays as a minimal overlay on the canvas.
 * @module renderer/hud
 */

export class HudRenderer {
  /**
   * @param {HTMLElement} roundEl - Round info overlay element (on canvas)
   * @param {HTMLElement} killfeedEl - Kill feed panel element (in tab)
   * @param {HTMLElement} playersEl - Players panel element (in tab)
   * @param {HTMLElement} lobbyEl - Lobby panel element (in tab)
   * @param {HTMLElement} statusEl - Connection status element
   */
  constructor(roundEl, killfeedEl, playersEl, lobbyEl, statusEl) {
    this.roundEl = roundEl;
    this.killfeedEl = killfeedEl;
    this.playersEl = playersEl;
    this.lobbyEl = lobbyEl;
    this.statusEl = statusEl;
    this.maxKills = 12;
    this._killLifetimeMs = 10000;
    this._seenKeys = new Set();
    this._activeEntries = [];
    this._lastPlayerHtml = '';
    this._lastLobbyHtml = '';
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
    this._updateRoundInfo(state);
    this._updateKillFeed(state.kill_feed || []);
    this._updatePlayers(state.bots || []);
    this._updateWaitingBots(state.waiting_bots || []);
  }

  /** @private */
  _updateLobbyInfo(state) {
    this.resetKillFeed();
    const players = state.players || [];
    const count = state.bots_connected || 0;

    // Minimal overlay: lobby status + countdown
    let statusText;
    if (state.countdown) {
      statusText = `LOBBY — Round in <span style="color:var(--accent-gold)">${state.countdown}s</span>`;
    } else {
      const needed = (state.bots_needed || 2) - count;
      statusText = `LOBBY — Waiting for <span style="color:var(--accent-gold)">${needed}</span> more`;
    }
    this.roundEl.innerHTML = `
      <div style="color:var(--accent-blue);margin-bottom:2px">${statusText}</div>
      <div>Players: <span style="color:var(--accent-gold)">${count}</span></div>
    `;

    // Lobby player list in the Lobby tab
    const html = players.map(p =>
      `<div class="player-entry">` +
        `<span style="color:${this._esc(p.avatar_color || '#fff')}">\u25CF</span> ` +
        `${this._esc(p.name)} ` +
        `<span style="color:var(--text-muted)">[${this._esc(p.weapon)}]</span>` +
      `</div>`
    ).join('');
    if (html !== this._lastLobbyHtml) {
      this.lobbyEl.innerHTML = html;
      this._lastLobbyHtml = html;
    }
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

  /** @private - Update player roster during gameplay (alive first, then dead, API order within each) */
  _updatePlayers(bots) {
    const alive = bots.filter(b => b.is_alive);
    const dead = bots.filter(b => !b.is_alive);
    const sorted = [...alive, ...dead];
    const html = sorted.map(b =>
      `<div class="player-entry${b.is_alive ? '' : ' dead'}">` +
        `<span style="color:${this._esc(b.avatar_color || '#fff')}">\u25CF</span> ` +
        `${this._esc(b.name)} ` +
        `<span style="color:var(--text-muted)">${b.is_alive ? Math.round(b.hp) + 'hp' : 'dead'}</span>` +
      `</div>`
    ).join('');
    if (html !== this._lastPlayerHtml) {
      this.playersEl.innerHTML = html;
      this._lastPlayerHtml = html;
    }
  }

  /** @private */
  _updateKillFeed(kills) {
    if (!kills || kills.length === 0) return;

    const newKills = [];
    for (const kill of kills) {
      const key = `${kill.killer}-${kill.victim}-${kill.tick}`;
      if (!this._seenKeys.has(key)) {
        this._seenKeys.add(key);
        newKills.push(kill);
      }
    }

    for (let i = newKills.length - 1; i >= 0; i--) {
      const kill = newKills[i];
      const el = this._createKillEntry(kill);
      this.killfeedEl.prepend(el);
      this._activeEntries.unshift({ key: `${kill.killer}-${kill.victim}-${kill.tick}`, el });
      setTimeout(() => this._removeEntry(el), this._killLifetimeMs);
    }

    while (this._activeEntries.length > this.maxKills) {
      const removed = this._activeEntries.pop();
      removed.el.remove();
    }
  }

  /** @private */
  _createKillEntry(kill) {
    const el = document.createElement('div');
    el.className = 'killfeed-entry killfeed-new';

    const killer = document.createElement('span');
    killer.className = 'killer';
    killer.textContent = kill.killer || '???';

    const sep = document.createElement('span');
    sep.style.color = 'var(--text-muted)';
    sep.textContent = ` ${this._weaponIcon(kill.weapon)} `;

    const victim = document.createElement('span');
    victim.className = 'victim';
    victim.textContent = kill.victim || '???';

    el.append(killer, sep, victim);
    return el;
  }

  /** @private */
  _removeEntry(el) {
    el.classList.add('killfeed-out');
    el.addEventListener('animationend', () => {
      el.remove();
      this._activeEntries = this._activeEntries.filter(e => e.el !== el);
    }, { once: true });
  }

  /** @private - Show bots waiting to join next round */
  _updateWaitingBots(waitingBots) {
    if (!waitingBots || waitingBots.length === 0) {
      if (this._lastLobbyHtml !== '') {
        this.lobbyEl.innerHTML = '<div style="color:var(--text-muted);padding:8px">No bots waiting</div>';
        this._lastLobbyHtml = '';
      }
      return;
    }
    const html = `<div style="color:var(--text-muted);padding:4px 0;font-size:0.8rem">Joining next round (${waitingBots.length})</div>` +
      waitingBots.map(p =>
        `<div class="player-entry">` +
          `<span style="color:${this._esc(p.avatar_color || '#fff')}">\u25CF</span> ` +
          `${this._esc(p.name)} ` +
          `<span style="color:var(--text-muted)">[${this._esc(p.weapon)}]</span>` +
        `</div>`
      ).join('');
    if (html !== this._lastLobbyHtml) {
      this.lobbyEl.innerHTML = html;
      this._lastLobbyHtml = html;
    }
  }

  /** Reset kill feed state (e.g. on new round). */
  resetKillFeed() {
    this._seenKeys.clear();
    this._activeEntries = [];
    this.killfeedEl.innerHTML = '';
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
