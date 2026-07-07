const apiBase = window.location.pathname.startsWith('/arena/') ? '/arena/api/v1' : '/api/v1';

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
    const res = await fetch(`${apiBase}/content`, { headers: { Accept: 'application/json' } });
    if (!res.ok) return;
    const data = await res.json();
    applyContentBlocks(data.blocks || {});
  } catch (_) {
    // Static fallback copy remains in the HTML when the API is unavailable.
  }
}

loadContentBlocks();
