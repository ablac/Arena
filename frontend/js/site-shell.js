'use strict';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'summary',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function visibleFocusable(root) {
  if (!root) return [];
  return Array.from(root.querySelectorAll(FOCUSABLE_SELECTOR)).filter((node) => {
    if (node.closest('[inert]')) return false;
    return !node.hidden && node.getAttribute('aria-hidden') !== 'true' && node.getClientRects().length > 0;
  });
}

function requestArenaResize() {
  requestAnimationFrame(() => {
    window.dispatchEvent(new Event('resize'));
    requestAnimationFrame(() => window.dispatchEvent(new Event('resize')));
  });
}

function setupCinemaMode() {
  const button = document.getElementById('fullscreen-btn');
  if (!button) return;

  let active = false;
  const render = () => {
    document.body.classList.toggle('site-cinema-mode', active);
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', String(active));
    button.textContent = active ? 'Exit Cinema' : 'Cinema Mode';
    requestArenaResize();
  };

  // app.js owns the legacy animated expand behavior. Capture this click first
  // so the arena-first shell can hide chrome without moving or recreating the
  // renderer canvas (and without redirecting a phone to a second site).
  document.addEventListener('click', (event) => {
    const target = event.target.closest?.('#fullscreen-btn');
    if (!target) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    active = !active;
    render();
  }, true);

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !active) return;
    active = false;
    render();
    button.focus({ preventScroll: true });
  });

  render();
}

function setupTelemetrySheet() {
  const shell = document.getElementById('arena-shell');
  const toggle = document.querySelector('[data-telemetry-toggle]');
  if (!shell || !toggle) return;

  const setOpen = (open) => {
    shell.classList.toggle('telemetry-open', open);
    toggle.setAttribute('aria-expanded', String(open));
    toggle.textContent = open ? 'Close feed' : 'Match feed';
  };

  toggle.addEventListener('click', () => setOpen(!shell.classList.contains('telemetry-open')));
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || !shell.classList.contains('telemetry-open')) return;
    setOpen(false);
    toggle.focus({ preventScroll: true });
  });
}

// Desktop/tablet minimize for the Live Feed panel — the HUD's Hide pattern
// applied to the telemetry sidebar. Inert at phone widths, where the panel is
// the bottom sheet driven by setupTelemetrySheet instead.
function setupTelemetryCollapse() {
  const shell = document.getElementById('arena-shell');
  const button = document.querySelector('[data-telemetry-collapse]');
  if (!shell || !button) return;

  const KEY = 'arenaTelemetryCollapsed';
  const setCollapsed = (collapsed) => {
    shell.classList.toggle('telemetry-collapsed', collapsed);
    button.setAttribute('aria-expanded', String(!collapsed));
    button.textContent = collapsed ? 'Show feed' : 'Hide';
    try { localStorage.setItem(KEY, collapsed ? '1' : '0'); } catch { /* private mode */ }
    requestArenaResize();
  };

  button.addEventListener('click', () => {
    setCollapsed(!shell.classList.contains('telemetry-collapsed'));
  });

  let saved = null;
  try { saved = localStorage.getItem(KEY); } catch { /* private mode */ }
  if (saved === '1') setCollapsed(true);
}

// Mirrors the header's spectator pill into the mobile menu sheet so the nav
// carries a live arena signal.
function setupDockLiveChip() {
  const chip = document.getElementById('dock-live-chip');
  const label = chip?.querySelector('[data-dock-chip-label]');
  const pill = document.getElementById('site-live-pill');
  if (!chip || !label || !pill) return;

  const sync = () => {
    chip.classList.toggle('connected', pill.classList.contains('connected'));
    const text = pill.textContent.replace(/^\s*Spectator\s*/i, '').trim();
    label.textContent = text ? text.charAt(0).toUpperCase() + text.slice(1) : 'Arena';
  };
  sync();
  new MutationObserver(sync).observe(pill, {
    childList: true,
    subtree: true,
    characterData: true,
    attributes: true,
    attributeFilter: ['class'],
  });
}

function setupCommandDock() {
  const dock = document.getElementById('site-command-dock');
  const toggle = document.querySelector('[data-site-menu-toggle]');
  const close = dock?.querySelector('[data-site-menu-close]');
  const backdrop = document.getElementById('overlay-backdrop');
  if (!dock || !toggle) return;

  const setOpen = (open, restoreFocus = false) => {
    dock.classList.toggle('is-open', open);
    document.body.classList.toggle('site-menu-open', open);
    toggle.setAttribute('aria-expanded', String(open));
    if (open) {
      const first = visibleFocusable(dock).find((node) => !node.matches('[data-site-menu-close]'));
      first?.focus({ preventScroll: true });
    } else if (restoreFocus) {
      toggle.focus({ preventScroll: true });
    }
  };

  toggle.addEventListener('click', () => setOpen(!dock.classList.contains('is-open')));
  close?.addEventListener('click', () => setOpen(false, true));
  backdrop?.addEventListener('click', () => {
    if (dock.classList.contains('is-open')) setOpen(false, true);
  });
  dock.addEventListener('click', (event) => {
    if (event.target.closest('[data-overlay-open]')) setOpen(false);
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && dock.classList.contains('is-open')) {
      setOpen(false, true);
      return;
    }
    if (event.key !== 'Tab' || !dock.classList.contains('is-open')) return;
    const focusable = visibleFocusable(dock);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    } else if (!dock.contains(document.activeElement)) {
      event.preventDefault();
      first.focus();
    }
  });
}

function topOpenOverlay(overlays) {
  const open = overlays.filter((overlay) => overlay.classList.contains('open'));
  return open.sort((a, b) => {
    const aIndex = Number.parseInt(a.style.zIndex || '80', 10);
    const bIndex = Number.parseInt(b.style.zIndex || '80', 10);
    return aIndex - bIndex;
  }).at(-1) || null;
}

function setupAccessibleOverlays() {
  const overlays = Array.from(document.querySelectorAll('.onboarding-overlay'));
  if (overlays.length === 0) return;

  const wasOpen = new WeakMap();
  overlays.forEach((overlay) => {
    const open = overlay.classList.contains('open');
    wasOpen.set(overlay, open);
    overlay.inert = !open;
  });

  const syncTriggerState = (overlay) => {
    const open = overlay.classList.contains('open');
    document.querySelectorAll(`[data-overlay-open="${overlay.id}"]`).forEach((trigger) => {
      trigger.setAttribute('aria-controls', overlay.id);
      trigger.setAttribute('aria-expanded', String(open));
    });
  };

  const syncOverlay = (overlay) => {
    const open = overlay.classList.contains('open');
    const changed = wasOpen.get(overlay) !== open;
    wasOpen.set(overlay, open);
    overlay.inert = !open;
    syncTriggerState(overlay);
    if (!changed) return;

    if (open) {
      requestAnimationFrame(() => {
        const drawer = overlay.querySelector('.onboarding-drawer');
        const preferred = drawer?.querySelector('.drawer-close') || visibleFocusable(drawer)[0];
        preferred?.focus({ preventScroll: true });
      });
      return;
    }

    const returnFocus = overlay._siteShellReturnFocus;
    if (returnFocus?.isConnected && !topOpenOverlay(overlays)) {
      requestAnimationFrame(() => returnFocus.focus({ preventScroll: true }));
    }
  };

  overlays.forEach((overlay) => {
    syncTriggerState(overlay);
    new MutationObserver(() => syncOverlay(overlay)).observe(overlay, {
      attributes: true,
      attributeFilter: ['class'],
    });
  });

  document.addEventListener('click', (event) => {
    const trigger = event.target.closest?.('[data-overlay-open]');
    if (!trigger) return;
    const overlay = document.getElementById(trigger.dataset.overlayOpen);
    if (overlay) overlay._siteShellReturnFocus = trigger;
  });

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const overlay = topOpenOverlay(overlays);
    if (!overlay) return;
    const drawer = overlay.querySelector('.onboarding-drawer');
    const focusable = visibleFocusable(drawer);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    } else if (!drawer.contains(document.activeElement)) {
      event.preventDefault();
      first.focus();
    }
  });
}

function initServiceBanner() {
  import('./service-status.js')
    .then(({ initServiceStatus }) => initServiceStatus())
    .catch((error) => console.warn('[SiteShell] Service status unavailable:', error));
}

document.addEventListener('DOMContentLoaded', () => {
  document.body.classList.add('site-shell-ready');
  setupCinemaMode();
  setupTelemetrySheet();
  setupTelemetryCollapse();
  setupCommandDock();
  setupAccessibleOverlays();
  setupDockLiveChip();
  initServiceBanner();
  requestArenaResize();
});
