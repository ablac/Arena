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
    btn.textContent = 'Generating...';
    try {
      const data = await generateKey();
      showKey(resultDiv, data);
      btn.textContent = 'Generate Another Key';
    } catch (err) {
      resultDiv.innerHTML = `<p style="color:var(--accent-red)">Error: ${escapeHtml(err.message)}</p>`;
      btn.textContent = 'Generate Key';
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
      <div class="copy-field">
        <input type="text" value="${escapeAttr(data.api_key)}" readonly id="key-display">
        <button onclick="document.getElementById('key-display').select();document.execCommand('copy');this.textContent='Copied!';setTimeout(()=>this.textContent='Copy',2000)">Copy</button>
      </div>
      <p class="keygen-warning">Save this key! It cannot be recovered.</p>
      <p style="margin-top:12px;font-size:0.9rem">
        Bot ID: <code style="color:var(--accent-blue)">${escapeHtml(data.bot_id)}</code>
      </p>
      <p style="margin-top:8px;font-size:0.9rem">
        <a href="#getting-started" style="color:var(--accent-blue)">Now configure your bot &rarr;</a>
      </p>
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
