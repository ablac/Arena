'use strict';

/**
 * Main application - wires up all modules.
 * @module app
 */

import { ArenaEngine } from './renderer/engine.js';
import { HudRenderer } from './renderer/hud.js';
import { SpectatorSocket } from './spectator-ws.js';
import { initLeaderboardWidget } from './leaderboard.js';
import { initKeyGenerator } from './key-generator.js';

const ARENA_WIDTH = 2000;
const ARENA_HEIGHT = 2000;

/** Boot the application when DOM is ready. */
document.addEventListener('DOMContentLoaded', async () => {
  mountOverlaySections();
  setupRevealAnimations();
  setupOverlays();

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
      updateHeroStatus(state);
      updateFollowDropdown(state);
    },
    (status) => hud.setStatus(status),
  );
  spectator.connect();

  // Controls
  setupControls(arenaEngine);

  // Leaderboard
  initLeaderboardWidgets();

  // Key generator
  const keygenEl = document.getElementById('keygen-card');
  if (keygenEl) initKeyGenerator(keygenEl);

  hud.onSelectBot = (botID) => {
    arenaEngine.selectBot(botID);
  };
  arenaEngine.onSelectBot = (botID) => {
    hud.setSelectedBot(botID);
  };

  // Smooth scroll for CTA
  document.querySelectorAll('a[href^="#"]').forEach((a) => {
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
  const arenaSection = document.getElementById('arena');
  const arenaShell = document.getElementById('arena-shell');
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
      engine.setAutoPan(active);
    });
  }

  const fullscreenBtn = document.getElementById('fullscreen-btn');
  if (fullscreenBtn && arenaSection && arenaShell) {
    let expanded = false;
    let animating = false;
    let restoreScrollY = 0;

    const setBtn = (isExpanded) => {
      fullscreenBtn.textContent = isExpanded ? 'Collapse View' : 'Expand View';
      fullscreenBtn.classList.toggle('active', isExpanded);
    };

    const getViewportInset = () => (window.innerWidth <= 768 ? 10 : 18);

    const setScrollbarComp = () => {
      const comp = Math.max(0, window.innerWidth - document.documentElement.clientWidth);
      document.documentElement.style.setProperty('--arena-scrollbar-comp', `${comp}px`);
    };

    const refreshArenaSize = () => {
      engine.engine?.resize();
      requestAnimationFrame(() => engine.engine?.resize());
      setTimeout(() => engine.engine?.resize(), 120);
      setTimeout(() => engine.engine?.resize(), 280);
    };

    const applyShellRect = (rect) => {
      arenaShell.style.top = `${rect.top}px`;
      arenaShell.style.left = `${rect.left}px`;
      arenaShell.style.width = `${rect.width}px`;
      arenaShell.style.height = `${rect.height}px`;
    };

    const clearShellRect = () => {
      arenaShell.style.removeProperty('top');
      arenaShell.style.removeProperty('left');
      arenaShell.style.removeProperty('width');
      arenaShell.style.removeProperty('height');
    };

    const getExpandedRect = () => {
      const inset = getViewportInset();
      return {
        top: inset,
        left: inset,
        width: window.innerWidth - (inset * 2),
        height: window.innerHeight - (inset * 2),
      };
    };

    const finishExpand = () => {
      expanded = true;
      animating = false;
      setBtn(true);
      refreshArenaSize();
    };

    const finishCollapse = () => {
      const settledRect = arenaSection.getBoundingClientRect();
      applyShellRect(settledRect);
      document.body.classList.remove('arena-fullscreen');
      window.scrollTo(0, restoreScrollY);

      requestAnimationFrame(() => {
        arenaShell.classList.remove('is-floating');
        arenaSection.classList.remove('is-floating');

        requestAnimationFrame(() => {
          clearShellRect();
          arenaSection.style.removeProperty('height');
          expanded = false;
          animating = false;
          setBtn(false);
          refreshArenaSize();
        });
      });
    };

    const transitionShell = (targetRect, onComplete) => {
      const handleEnd = (event) => {
        if (event.target !== arenaShell || event.propertyName !== 'width') return;
        arenaShell.removeEventListener('transitionend', handleEnd);
        onComplete();
      };

      arenaShell.addEventListener('transitionend', handleEnd);
      requestAnimationFrame(() => {
        applyShellRect(targetRect);
      });
    };

    const expandShell = () => {
      if (animating || expanded) return;
      animating = true;
      restoreScrollY = window.scrollY;
      setScrollbarComp();

      const startRect = arenaShell.getBoundingClientRect();
      arenaSection.style.height = `${startRect.height}px`;
      arenaSection.classList.add('is-floating');
      document.body.classList.add('arena-fullscreen');
      arenaShell.classList.add('is-floating');
      applyShellRect(startRect);

      // Force layout so the browser has a stable start box before animating.
      arenaShell.getBoundingClientRect();
      transitionShell(getExpandedRect(), finishExpand);
    };

    const collapseShell = () => {
      if (animating || !expanded) return;
      animating = true;
      const targetRect = arenaSection.getBoundingClientRect();
      applyShellRect(arenaShell.getBoundingClientRect());
      transitionShell(targetRect, finishCollapse);
    };

    fullscreenBtn.addEventListener('click', () => {
      if (expanded) {
        collapseShell();
        return;
      }
      expandShell();
    });

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && expanded) {
        collapseShell();
      }
    });

    window.addEventListener('resize', () => {
      if (!expanded || animating) return;
      applyShellRect(getExpandedRect());
      refreshArenaSize();
    });
  }
}

/** @private Populate follow dropdown with current bots. */
let lastBotList = '';
function updateFollowDropdown(state) {
  const select = document.getElementById('follow-bot');
  if (!select || !state.bots) return;
  const names = state.bots.filter((b) => b.is_alive).map((b) => b.name).sort().join(',');
  if (names === lastBotList) return;
  lastBotList = names;
  const current = select.value;
  select.innerHTML = '<option value="">None</option>';
  state.bots.filter((b) => b.is_alive).sort((a, b) => a.name.localeCompare(b.name)).forEach((bot) => {
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
  tabs.forEach((tab) => {
    tab.addEventListener('click', () => {
      tabs.forEach((t) => t.classList.remove('active'));
      panels.forEach((p) => p.classList.remove('active'));
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
    syncHeroSummary({
      phase: data.status || 'connecting',
      botsConnected: data.bots_connected,
      botsAlive: data.bots_alive,
    });
  } catch {
    // ignore
  }
}

function updateHeroStatus(state) {
  if (!state) return;
  if (state.type === 'lobby_state') {
    syncHeroSummary({
      phase: state.countdown ? 'lobby countdown' : 'lobby',
      botsConnected: state.bots_connected || 0,
      botsAlive: 0,
    });
    return;
  }
  if (!state.bots) return;
  syncHeroSummary({
    phase: 'active round',
    botsConnected: state.bots.length,
    botsAlive: state.bots.filter((bot) => bot.is_alive).length,
  });
}

function syncHeroSummary({ phase, botsConnected, botsAlive }) {
  const phaseEl = document.getElementById('hero-phase');
  const botsEl = document.getElementById('hero-bots');
  const aliveEl = document.getElementById('hero-alive');
  if (phaseEl && phase) phaseEl.textContent = titleCase(phase);
  if (botsEl && Number.isFinite(botsConnected)) botsEl.textContent = `${botsConnected}`;
  if (aliveEl && Number.isFinite(botsAlive)) aliveEl.textContent = `${botsAlive}`;
}

function setupRevealAnimations() {
  const nodes = Array.from(document.querySelectorAll('.reveal-on-scroll'));
  if (nodes.length === 0) return;
  if (!('IntersectionObserver' in window)) {
    nodes.forEach((node) => node.classList.add('is-visible'));
    return;
  }

  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting) return;
      entry.target.classList.add('is-visible');
      observer.unobserve(entry.target);
    });
  }, { threshold: 0.18, rootMargin: '0px 0px -60px 0px' });

  nodes.forEach((node) => observer.observe(node));
}

function titleCase(value) {
  return String(value)
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function mountOverlaySections() {
  const apiReference = document.getElementById('api-reference');
  const apiSlot = document.getElementById('onboarding-api-slot');
  if (apiReference && apiSlot) {
    apiSlot.appendChild(apiReference);
    apiReference.classList.add('overlay-section-host');
  }

  document.body.classList.add('overlay-content-mounted');
}

function initLeaderboardWidgets() {
  const widgets = [
    {
      root: document.getElementById('standings-boards'),
      modeTabsContainer: document.getElementById('standings-mode-tabs'),
      sortTabsContainer: document.getElementById('standings-tabs'),
      podiumEl: document.getElementById('standings-podium'),
      leaderboardBody: document.getElementById('standings-body'),
      bountyBody: document.getElementById('standings-bounty-body'),
      weaponPodiumEl: document.getElementById('standings-weapon-podium'),
      weaponBody: document.getElementById('standings-weapon-body'),
      weaponUpdatedEl: document.getElementById('standings-weapon-updated'),
      limit: 20,
    },
  ];

  widgets.forEach((widget) => {
    if (!widget.root || !widget.modeTabsContainer || !widget.sortTabsContainer || !widget.podiumEl || !widget.leaderboardBody || !widget.bountyBody || !widget.weaponPodiumEl || !widget.weaponBody || !widget.weaponUpdatedEl) {
      return;
    }

    initLeaderboardWidget(widget);
  });
}

function setupOverlays() {
  const onboardingOverlay = document.getElementById('onboarding-overlay');
  const autoTrigger = document.querySelector('.arena-controls') || document.getElementById('arena');
  const openButtons = Array.from(document.querySelectorAll('[data-overlay-open]'));
  const closeButtons = Array.from(document.querySelectorAll('[data-close-overlay]'));
  let autoOpened = false;

  const openOverlay = (overlayId, targetSelector) => {
    const overlay = document.getElementById(overlayId);
    const drawer = overlay?.querySelector('.onboarding-drawer');
    if (!overlay || !drawer) return;

    document.querySelectorAll('.onboarding-overlay.open').forEach((openNode) => {
      openNode.classList.remove('open');
      openNode.setAttribute('aria-hidden', 'true');
    });

    overlay.classList.add('open');
    overlay.setAttribute('aria-hidden', 'false');
    document.body.classList.add('onboarding-open');

    if (!targetSelector) {
      drawer.scrollTo({ top: 0, behavior: 'smooth' });
    }

    if (targetSelector) {
      const target = document.querySelector(targetSelector);
      if (target) {
        requestAnimationFrame(() => {
          const targetTop = target.getBoundingClientRect().top - drawer.getBoundingClientRect().top + drawer.scrollTop - 18;
          drawer.scrollTo({ top: Math.max(0, targetTop), behavior: 'smooth' });
        });
      }
    }
  };

  const closeOverlay = (overlayId) => {
    const overlay = document.getElementById(overlayId);
    if (!overlay) return;
    overlay.classList.remove('open');
    overlay.setAttribute('aria-hidden', 'true');
    if (!document.querySelector('.onboarding-overlay.open')) {
      document.body.classList.remove('onboarding-open');
    }
  };

  openButtons.forEach((button) => {
    button.addEventListener('click', (event) => {
      event.preventDefault();
      openOverlay(button.dataset.overlayOpen, button.dataset.overlayTarget);
    });
  });

  closeButtons.forEach((button) => {
    button.addEventListener('click', () => closeOverlay(button.dataset.closeOverlay));
  });

  document.querySelectorAll('.onboarding-overlay').forEach((overlay) => {
    overlay.addEventListener('click', (event) => {
      if (event.target === overlay || event.target.classList.contains('onboarding-scrim')) {
        closeOverlay(overlay.id);
      }
    });
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      document.querySelectorAll('.onboarding-overlay.open').forEach((overlay) => closeOverlay(overlay.id));
    }
  });

  if (!onboardingOverlay || !autoTrigger) return;

  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting || autoOpened) return;
      if (window.scrollY < window.innerHeight * 0.65) return;
      autoOpened = true;
      openOverlay('onboarding-overlay');
      observer.disconnect();
    });
  }, { threshold: 0.28 });

  observer.observe(autoTrigger);
}
