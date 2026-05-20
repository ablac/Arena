'use strict';

/**
 * API key generation and display.
 * @module key-generator
 */

/**
 * Initialize the key generator UI.
 * @param {HTMLElement} container - The keygen card container
 */
export function initKeyGenerator(container) {
  const btn = container.querySelector('.keygen-btn');
  const resultDiv = container.querySelector('.keygen-result');
  if (!btn || !resultDiv) return;

  btn.addEventListener('click', async () => {
    btn.disabled = true;
    btn.textContent = 'Generating key...';
    try {
      const data = await generateKey();
      showKey(resultDiv, data);
      btn.textContent = 'Generate another key';
    } catch (err) {
      resultDiv.innerHTML = `<p class="keygen-error">Error: ${escapeHtml(err.message)}</p>`;
      btn.textContent = 'Generate API key';
    }
    btn.disabled = false;
  });
}

/**
 * Call the key generation API.
 * @returns {Promise<{api_key: string, bot_id: string}>}
 */
async function generateKey() {
  const baseUrl = window.location.origin;
  const resp = await fetch(`${baseUrl}/api/v1/keys/generate`, { method: 'POST' });
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}));
    throw new Error(body.detail || `HTTP ${resp.status}`);
  }
  return resp.json();
}

/**
 * Display the generated key with copy functionality.
 * @param {HTMLElement} container - Result container
 * @param {Object} data - API response
 */
function showKey(container, data) {
  container.innerHTML = `
    <div class="keygen-success">
      <div class="keygen-result-header">
        <span class="keygen-badge">Credential ready</span>
      </div>
      <div class="copy-field keygen-copy-field">
        <input type="text" value="${escapeAttr(data.api_key)}" readonly id="key-display">
        <button onclick="document.getElementById('key-display').select();document.execCommand('copy');this.textContent='Copied';setTimeout(()=>this.textContent='Copy',2000)">Copy</button>
      </div>
      <div class="keygen-meta">
        <p class="keygen-warning">Store this key now. It cannot be recovered later.</p>
        <p class="keygen-bot-id">Bot ID: <code>${escapeHtml(data.bot_id)}</code></p>
      </div>
      <a class="keygen-next-link" href="#onboarding-setup">Continue to bot configuration</a>
    </div>`;
}

/** @private */
function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

/** @private */
function escapeAttr(str) {
  return str.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
