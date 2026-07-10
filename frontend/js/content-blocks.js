import { apiBase } from './paths.js?v=20260710a';

const publicAPIBase = apiBase();

function applyContentBlocks(blocks) {
  document.querySelectorAll('[data-content-key]').forEach((el) => {
    const key = el.getAttribute('data-content-key');
    if (!key || !Object.prototype.hasOwnProperty.call(blocks, key)) return;
    const value = blocks[key];
    const attr = el.getAttribute('data-content-attr');
    if (attr) {
      el.setAttribute(attr, value);
    } else {
      el.textContent = value;
    }
  });
}

async function loadContentBlocks() {
  try {
    const res = await fetch(`${publicAPIBase}/content`, { headers: { Accept: 'application/json' } });
    if (!res.ok) return;
    const data = await res.json();
    applyContentBlocks(data.blocks || {});
  } catch (_) {
    // Static fallback copy remains in the HTML when the API is unavailable.
  }
}

loadContentBlocks();
