'use strict';

/**
 * Blocking Terms of Service / Privacy Policy / Acceptable Use consent gate.
 * Shown once, before a visitor's first sign-in attempt or their first API
 * key generation, whichever happens first. Self-contained (injects its own
 * <style> and <dialog>) so it renders identically on pages that load
 * arena.css and ones that don't (the dashboard iframe has its own separate
 * stylesheet).
 * @module consent-gate
 */

import { apiPath } from './paths.js?v=20260710a';

const CONSENT_VERSION = '2026-07-14';
const STORAGE_KEY = 'arena_consent_accepted_v1';
const DIALOG_ID = 'arena-consent-gate-dialog';

let pendingPrompt = null;

function hasAccepted() {
  try {
    return localStorage.getItem(STORAGE_KEY) === CONSENT_VERSION;
  } catch (err) {
    return false;
  }
}

function markAccepted() {
  try {
    localStorage.setItem(STORAGE_KEY, CONSENT_VERSION);
  } catch (err) {
    // Private browsing / storage disabled: the gate will simply ask again
    // next visit, which is a safe direction to fail in.
  }
}

// Resolved relative to this module's own URL (not the host page's), so the
// links are correct whether the gate is loaded from /, /dashboard/, /m/, or
// /shop/, and whether the whole site is mounted at / or under /arena/.
function legalHref(page) {
  return new URL(`../legal/${page}`, import.meta.url).pathname;
}

function injectStyle() {
  if (document.getElementById('arena-consent-gate-style')) return;
  const style = document.createElement('style');
  style.id = 'arena-consent-gate-style';
  style.textContent = `
    #${DIALOG_ID} {
      max-width: 460px;
      width: calc(100vw - 40px);
      padding: 0;
      border: 1px solid rgba(126, 166, 207, 0.28);
      border-radius: 12px;
      background: #0c1825;
      color: #ecf4ff;
      font-family: 'Space Grotesk', system-ui, sans-serif;
      box-shadow: 0 24px 60px rgba(0, 0, 0, 0.55);
    }
    #${DIALOG_ID}::backdrop {
      background: rgba(4, 9, 16, 0.72);
    }
    #${DIALOG_ID} .acg-body { padding: 22px 24px; }
    #${DIALOG_ID} h2 {
      margin: 0 0 10px;
      font-size: 1.1rem;
      letter-spacing: 0.01em;
    }
    #${DIALOG_ID} p {
      margin: 0 0 14px;
      font-size: 0.86rem;
      line-height: 1.5;
      color: #b9c8d8;
    }
    #${DIALOG_ID} .acg-links {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-bottom: 18px;
    }
    #${DIALOG_ID} .acg-links a {
      color: #47d7ff;
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.72rem;
      text-decoration: none;
      border: 1px solid rgba(126, 166, 207, 0.28);
      border-radius: 6px;
      padding: 6px 10px;
      white-space: nowrap;
    }
    #${DIALOG_ID} .acg-links a:hover { border-color: #47d7ff; }
    #${DIALOG_ID} .acg-actions {
      display: flex;
      justify-content: flex-end;
      gap: 10px;
    }
    #${DIALOG_ID} button {
      font-family: 'IBM Plex Mono', monospace;
      font-size: 0.78rem;
      padding: 9px 16px;
      border-radius: 6px;
      cursor: pointer;
    }
    #${DIALOG_ID} .acg-decline {
      background: transparent;
      border: 1px solid rgba(126, 166, 207, 0.28);
      color: #b9c8d8;
    }
    #${DIALOG_ID} .acg-decline:hover { border-color: rgba(126, 166, 207, 0.5); }
    #${DIALOG_ID} .acg-accept {
      background: #47d7ff;
      border: 1px solid #47d7ff;
      color: #06111c;
      font-weight: 600;
    }
  `;
  document.head.appendChild(style);
}

function buildDialog() {
  injectStyle();
  const dialog = document.createElement('dialog');
  dialog.id = DIALOG_ID;
  dialog.innerHTML = `
    <div class="acg-body">
      <h2>Before you continue</h2>
      <p>The Arena needs your agreement to a few short documents before you sign in or generate an API key. Please review them, then accept to continue.</p>
      <div class="acg-links">
        <a href="${legalHref('terms.html')}" target="_blank" rel="noopener">Terms of Service</a>
        <a href="${legalHref('privacy.html')}" target="_blank" rel="noopener">Privacy Policy</a>
        <a href="${legalHref('acceptable-use.html')}" target="_blank" rel="noopener">Acceptable Use</a>
      </div>
      <div class="acg-actions">
        <button type="button" class="acg-decline">Cancel</button>
        <button type="button" class="acg-accept">I Agree, Continue</button>
      </div>
    </div>
  `;
  document.body.appendChild(dialog);
  return dialog;
}

async function beaconAcceptance() {
  try {
    await fetch(apiPath('/consent/accept'), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: CONSENT_VERSION }),
    });
  } catch (err) {
    // Best-effort audit trail only; the accepted-locally state already
    // resolved and must never be blocked on network reachability.
  }
}

/**
 * Resolves true immediately if consent was already recorded for the current
 * version. Otherwise shows the blocking dialog and resolves with the
 * visitor's choice. Callers should abort whatever action triggered the gate
 * (sign-in, key generation) when this resolves false.
 * @returns {Promise<boolean>}
 */
export function ensureConsent() {
  if (hasAccepted()) return Promise.resolve(true);
  if (pendingPrompt) return pendingPrompt;

  pendingPrompt = new Promise((resolve) => {
    const dialog = buildDialog();
    const finish = (accepted) => {
      dialog.close();
      dialog.remove();
      pendingPrompt = null;
      resolve(accepted);
    };
    dialog.querySelector('.acg-accept').addEventListener('click', () => {
      markAccepted();
      beaconAcceptance();
      finish(true);
    });
    dialog.querySelector('.acg-decline').addEventListener('click', () => finish(false));
    dialog.addEventListener('cancel', (event) => {
      event.preventDefault();
      finish(false);
    });
    dialog.showModal();
  });
  return pendingPrompt;
}

// Compatibility surface for classic (non-module) inline scripts, e.g. the
// dashboard's login flow, which cannot use ES `import`. Module consumers
// should use the named export above.
if (typeof window !== 'undefined') {
  window.ArenaConsentGate = Object.freeze({ ensureConsent });
}
