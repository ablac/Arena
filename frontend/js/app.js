'use strict';

/**
 * Main application — wires up all modules.
 * @module app
 */

import { ArenaEngine } from './renderer/engine.js';
import { HudRenderer } from './renderer/hud.js';
import { Minimap } from './renderer/minimap.js';
import { SpectatorSocket } from './spectator-ws.js';
import { initLeaderboard } from './leaderboard.js';
import { initKeyGenerator } from './key-generator.js';

const ARENA_WIDTH = 2000;
const ARENA_HEIGHT = 2000;

/** Boot the application when DOM is ready. */
document.addEventListener('DOMContentLoaded', async () => {
  // Arena renderer
  const canvas = document.getElementById('arena-canvas');
  const arenaEngine = new ArenaEngine(canvas, {
    arenaWidth: ARENA_WIDTH, arenaHeight: ARENA_HEIGHT,
  });

  // HUD
  const hud = new HudRenderer(
    document.getElementById('hud-round'),
    document.getElementById('hud-killfeed'),
    document.getElementById('ws-status'),
  );

  // Minimap
  const minimapContainer = document.querySelector('.arena-container');
  const minimap = new Minimap(minimapContainer, ARENA_WIDTH, ARENA_HEIGHT);

  // Initialize Babylon engine
  try {
    await arenaEngine.init();
    console.log('[App] Arena engine initialized');
  } catch (err) {
    console.error('[App] Engine init failed:', err);
  }

  // Spectator WebSocket
  const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${wsProtocol}//${window.location.host}/ws/watch`;
  const spectator = new SpectatorSocket(wsUrl,
    (state) => {
      arenaEngine.setState(state);
      hud.updateState(state);
      minimap.update(state);
      updateBotCount(state);
      updateFollowDropdown(state);
    },
    (status) => hud.setStatus(status),
  );
  spectator.connect();

  // Controls
  setupControls(arenaEngine, spectator);

  // Leaderboard
  const tabsEl = document.getElementById('leaderboard-tabs');
  const tbodyEl = document.getElementById('leaderboard-body');
  if (tabsEl && tbodyEl) initLeaderboard(tabsEl, tbodyEl);

  // Key generator
  const keygenEl = document.getElementById('keygen-card');
  if (keygenEl) initKeyGenerator(keygenEl);

  // Smooth scroll for CTA
  document.querySelectorAll('a[href^="#"]').forEach(a => {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      const target = document.querySelector(a.getAttribute('href'));
      if (target) target.scrollIntoView({ behavior: 'smooth' });
    });
  });

  // Fetch initial arena status
  fetchArenaStatus();
});

/** @private Setup arena controls. */
function setupControls(engine) {
  const zoomSlider = document.getElementById('zoom-slider');
  if (zoomSlider) {
    zoomSlider.addEventListener('input', (e) => {
      engine.setZoom(parseFloat(e.target.value));
      document.getElementById('zoom-value').textContent = `${e.target.value}x`;
    });
  }

  const followSelect = document.getElementById('follow-bot');
  if (followSelect) {
    followSelect.addEventListener('change', (e) => {
      engine.followBot(e.target.value || null);
    });
  }

  const autoPanBtn = document.getElementById('auto-pan');
  if (autoPanBtn) {
    autoPanBtn.addEventListener('click', () => {
      const active = autoPanBtn.classList.toggle('active');
      autoPanBtn.style.borderColor = active ? 'var(--accent-blue)' : 'var(--border-color)';
      engine.setAutoPan(active);
    });
  }

  const fullscreenBtn = document.getElementById('fullscreen-btn');
  if (fullscreenBtn) {
    fullscreenBtn.addEventListener('click', () => {
      const container = document.querySelector('.arena-container');
      container.classList.toggle('fullscreen');
      engine.engine?.resize();
    });
  }
}

/** @private Update live bot count in hero. */
function updateBotCount(state) {
  const el = document.getElementById('live-count');
  if (!el || !state.bots) return;
  const alive = state.bots.filter(b => b.is_alive).length;
  el.textContent = `${alive} bot${alive !== 1 ? 's' : ''} fighting right now`;
}

/** @private Populate follow dropdown with current bots. */
let lastBotList = '';
function updateFollowDropdown(state) {
  const select = document.getElementById('follow-bot');
  if (!select || !state.bots) return;
  const names = state.bots.filter(b => b.is_alive).map(b => b.name).sort().join(',');
  if (names === lastBotList) return;
  lastBotList = names;
  const current = select.value;
  select.innerHTML = '<option value="">None</option>';
  state.bots.filter(b => b.is_alive).sort((a, b) => a.name.localeCompare(b.name)).forEach(bot => {
    const opt = document.createElement('option');
    opt.value = bot.bot_id;
    opt.textContent = bot.name;
    select.appendChild(opt);
  });
  select.value = current;
}

/** @private Fetch arena status for footer stats. */
async function fetchArenaStatus() {
  try {
    const resp = await fetch(`${window.location.origin}/api/v1/arena/status`);
    if (!resp.ok) return;
    const data = await resp.json();
    const el = document.getElementById('footer-stats');
    if (el) {
      el.textContent = `${data.bots_connected} bots connected | Round ${data.round_number}`;
    }
  } catch { /* ignore */ }
}
