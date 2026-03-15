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
    document.getElementById('hud-players'),
    document.getElementById('hud-lobby'),
    document.getElementById('ws-status'),
  );

  // Arena info tabs
  setupArenaTabs();

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
  const wsUrl = `${wsProtocol}//${window.location.host}/ws/spectator`;
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
  const zoomLabel = document.getElementById('zoom-value');
  if (zoomSlider) {
    zoomSlider.addEventListener('input', (e) => {
      engine.setZoom(parseFloat(e.target.value));
    });
    // Sync slider when mouse wheel changes zoom
    if (engine.camera) {
      engine.camera.onZoomChange = (z) => {
        zoomSlider.value = z;
        if (zoomLabel) zoomLabel.textContent = `${z.toFixed(1)}x`;
      };
    }
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
    let savedRect = null;
    let animating = false;

    function setBtn(isMax) {
      fullscreenBtn.textContent = isMax ? 'Exit Fullscreen' : 'Fullscreen';
      fullscreenBtn.style.background = isMax ? '#e74c3c' : '';
      fullscreenBtn.style.color = isMax ? '#fff' : '';
      fullscreenBtn.style.borderColor = isMax ? '#e74c3c' : '';
    }

    function zoomIn() {
      if (animating) return;
      animating = true;
      const section = document.getElementById('arena');
      const rect = section.getBoundingClientRect();
      savedRect = { top: rect.top, left: rect.left, width: rect.width, height: rect.height };

      section.classList.add('animating');
      section.style.top = rect.top + 'px';
      section.style.left = rect.left + 'px';
      section.style.width = rect.width + 'px';
      section.style.height = rect.height + 'px';
      section.style.margin = '0';
      section.style.padding = '0';
      section.style.maxWidth = 'none';
      document.body.style.overflow = 'hidden';

      section.offsetHeight;
      section.style.top = '0';
      section.style.left = '0';
      section.style.width = '100vw';
      section.style.height = '100vh';
      section.style.borderRadius = '0';

      setTimeout(() => {
        section.classList.remove('animating');
        section.classList.add('maximized');
        section.style.cssText = '';
        setBtn(true);
        engine.engine?.resize();
        animating = false;
      }, 420);
    }

    function zoomOut() {
      if (animating) return;
      animating = true;
      const section = document.getElementById('arena');
      if (!savedRect) savedRect = { top: 200, left: 100, width: 800, height: 500 };

      section.classList.remove('maximized');
      section.classList.add('animating');
      section.style.top = '0';
      section.style.left = '0';
      section.style.width = '100vw';
      section.style.height = '100vh';
      section.style.margin = '0';
      section.style.padding = '0';
      section.style.maxWidth = 'none';
      section.style.borderRadius = '0';

      section.offsetHeight;
      section.style.top = savedRect.top + 'px';
      section.style.left = savedRect.left + 'px';
      section.style.width = savedRect.width + 'px';
      section.style.height = savedRect.height + 'px';
      section.style.borderRadius = '12px';

      setTimeout(() => {
        section.classList.remove('animating');
        section.style.cssText = '';
        document.body.style.overflow = '';
        setBtn(false);
        engine.engine?.resize();
        animating = false;
      }, 420);
    }

    fullscreenBtn.addEventListener('click', () => {
      const section = document.getElementById('arena');
      if (section.classList.contains('maximized')) {
        zoomOut();
      } else if (!animating) {
        zoomIn();
      }
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        const section = document.getElementById('arena');
        if (section && section.classList.contains('maximized')) {
          zoomOut();
        }
      }
    });
  }
}

/** @private Update live bot count in hero. */
function updateBotCount(state) {
  const el = document.getElementById('live-count');
  if (!el) return;
  if (state.type === 'lobby_state') {
    const n = state.bots_connected || 0;
    el.textContent = `${n} bot${n !== 1 ? 's' : ''} in lobby — waiting for battle`;
    return;
  }
  if (!state.bots) return;
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

/** @private Wire up the arena info tab bar. */
function setupArenaTabs() {
  const tabs = document.querySelectorAll('.arena-tab');
  const panels = document.querySelectorAll('.arena-tab-panel');
  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      tabs.forEach(t => t.classList.remove('active'));
      panels.forEach(p => p.classList.remove('active'));
      tab.classList.add('active');
      const panel = document.getElementById(`tab-${tab.dataset.tab}`);
      if (panel) panel.classList.add('active');
    });
  });
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
