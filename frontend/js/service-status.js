'use strict';

import { apiPath } from './paths.js?v=20260710a';

let activeController = null;
let pendingStatus = null;

function findDefaultRoot() {
  return document.getElementById('siteBroadcast') ||
    document.getElementById('service-status-banner') ||
    document.querySelector('[data-service-status-root]');
}

function isExpired(notice) {
  if (!notice || !notice.expires_at) return false;
  const expires = Date.parse(notice.expires_at);
  return Number.isFinite(expires) && expires <= Date.now();
}

function visibleNotice(status) {
  if (status?.maintenance && !isExpired(status.maintenance)) {
    return { kind: 'maintenance', notice: status.maintenance };
  }
  if (status?.broadcast && !isExpired(status.broadcast)) {
    return { kind: 'broadcast', notice: status.broadcast };
  }
  return null;
}

function dismissalKey(notice) {
  return `arena-service-status-dismissed:${notice.id}`;
}

function wasDismissed(notice) {
  try { return sessionStorage.getItem(dismissalKey(notice)) === '1'; } catch { return false; }
}

function markDismissed(notice) {
  try { sessionStorage.setItem(dismissalKey(notice), '1'); } catch { /* storage can be disabled */ }
}

/**
 * Mount the public service-status banner and begin authoritative REST polling.
 * WebSocket control messages can be applied immediately through handleStatus.
 */
export function initServiceStatus(options = {}) {
  const root = options.root || findDefaultRoot();
  const pollIntervalMs = Number(options.pollIntervalMs) > 0 ? Number(options.pollIntervalMs) : 15000;
  if (activeController && activeController.root === root) return activeController;
  if (activeController) activeController.destroy();

  const messageEl = root?.querySelector('[data-service-status-message], [data-service-status]') || null;
  const dismissButton = root?.querySelector('[data-service-status-dismiss]') || null;
  let current = null;
  let currentVisible = null;
  let timer = null;
  let destroyed = false;

  function render(status) {
    current = status || null;
    currentVisible = visibleNotice(current);
    if (!root || !messageEl) return;
    if (!currentVisible || (currentVisible.kind === 'broadcast' && wasDismissed(currentVisible.notice))) {
      root.hidden = true;
      root.removeAttribute('data-kind');
      root.removeAttribute('data-severity');
      messageEl.textContent = '';
      return;
    }

    const { kind, notice } = currentVisible;
    root.dataset.kind = kind;
    root.dataset.severity = notice.severity || 'info';
    messageEl.textContent = notice.message || '';
    root.hidden = false;
    if (dismissButton) dismissButton.hidden = kind === 'maintenance';
  }

  async function refresh() {
    if (destroyed) return null;
    try {
      const response = await fetch(apiPath('/service-status'), {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      });
      if (!response.ok) return null;
      const status = await response.json();
      handleStatus(status);
      return status;
    } catch {
      // Keep the last notice visible while the server is actually restarting.
      return null;
    }
  }

  function handleStatus(status) {
    if (!status || status.type !== 'service_status') return false;
    if (current && Number(status.revision) < Number(current.revision)) return false;
    render(status);
    return true;
  }

  function dismiss() {
    if (!currentVisible || currentVisible.kind !== 'broadcast') return;
    markDismissed(currentVisible.notice);
    render(current);
  }

  function destroy() {
    destroyed = true;
    if (timer) clearInterval(timer);
    timer = null;
    dismissButton?.removeEventListener('click', dismiss);
    if (activeController === controller) activeController = null;
  }

  const controller = {
    root,
    handleStatus,
    refresh,
    destroy,
    get current() { return current; },
  };
  activeController = controller;
  dismissButton?.addEventListener('click', dismiss);
  if (pendingStatus) {
    handleStatus(pendingStatus);
    pendingStatus = null;
  }
  refresh();
  timer = setInterval(refresh, pollIntervalMs);
  return controller;
}

// Singleton bridge used by the spectator stream. It safely buffers a status
// received before the site/mobile shell has mounted its banner.
export function handleServiceStatus(status) {
  if (activeController) return activeController.handleStatus(status);
  if (status?.type === 'service_status') {
    pendingStatus = status;
    return true;
  }
  return false;
}

function autoInit() {
  if (!activeController && findDefaultRoot()) initServiceStatus();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', autoInit, { once: true });
} else {
  autoInit();
}
