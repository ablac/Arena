'use strict';

import { apiPath } from './paths.js?v=20260710a';
import {
  createLatestRequestGate,
  onArenaAPIKeyClear,
  requestArenaAPIKeyClear,
} from './credential-events.js?v=20260710a';

const SLOT_LABELS = {
  bot_skin: 'Chassis Skins',
  weapon_skin: 'Weapon Finishes',
  attachment: 'Attachments',
};

function formatPrice(item) {
  if (item.is_free) return 'Free';
  const cents = Number(item.price_cents || 0);
  const currency = item.currency || 'USD';
  try {
    return new Intl.NumberFormat(undefined, {style: 'currency', currency}).format(cents / 100);
  } catch (_) {
    return `$${(cents / 100).toFixed(2)}`;
  }
}

async function readJSON(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(body.error || body.detail || `Request failed (${response.status})`);
  }
  return body;
}

function itemCard(item, authenticated, onEquip) {
  const card = document.createElement('article');
  card.className = `cosmetic-item rarity-${item.rarity || 'common'}`;

  const header = document.createElement('div');
  header.className = 'cosmetic-item-header';
  const name = document.createElement('strong');
  name.textContent = item.name || item.id || 'Cosmetic';
  const price = document.createElement('span');
  price.className = 'cosmetic-price';
  price.textContent = formatPrice(item);
  header.append(name, price);

  const description = document.createElement('p');
  description.textContent = item.description || 'Presentation-only Arena customization.';

  const footer = document.createElement('div');
  footer.className = 'cosmetic-item-footer';
  const badge = document.createElement('span');
  badge.className = 'cosmetic-state';
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'btn btn-secondary cosmetic-equip';

  if (!authenticated) {
    badge.textContent = item.is_free ? 'Starter' : 'Preview';
    button.textContent = 'Load bot key';
    button.disabled = true;
  } else if (item.equipped) {
    badge.textContent = 'Equipped';
    button.textContent = 'Equipped';
    button.disabled = true;
  } else if (item.owned) {
    badge.textContent = 'Owned';
    button.textContent = 'Equip';
    button.addEventListener('click', () => onEquip(item, button));
  } else {
    badge.textContent = item.is_purchasable ? 'Locked' : 'Preview only';
    button.textContent = item.is_purchasable ? 'Checkout unavailable' : 'Coming soon';
    button.disabled = true;
  }

  footer.append(badge, button);
  card.append(header, description, footer);
  return card;
}

function renderCatalog(root, items, authenticated, onEquip) {
  root.replaceChildren();
  for (const [slot, label] of Object.entries(SLOT_LABELS)) {
    const section = document.createElement('section');
    section.className = 'cosmetic-slot';
    const heading = document.createElement('h4');
    heading.textContent = label;
    const grid = document.createElement('div');
    grid.className = 'cosmetic-items';
    const slotItems = items.filter(item => item.slot === slot);
    for (const item of slotItems) grid.appendChild(itemCard(item, authenticated, onEquip));
    section.append(heading, grid);
    root.appendChild(section);
  }
}

/**
 * Mount the starter cosmetic catalog and authenticated equip UI.
 * The API key stays only in the input/closure and is never persisted.
 */
export function initCosmeticsPanel(container) {
  if (!container) return null;
  const keyInput = container.querySelector('[data-cosmetic-key]');
  const loadButton = container.querySelector('[data-cosmetic-load]');
  const clearButton = container.querySelector('[data-cosmetic-clear]');
  const status = container.querySelector('[data-cosmetic-status]');
  const catalogRoot = container.querySelector('[data-cosmetic-catalog]');
  const checkoutState = container.querySelector('[data-cosmetic-checkout]');
  if (!keyInput || !loadButton || !status || !catalogRoot) return null;

  let activeKey = '';
  let busy = false;
  const requestGate = createLatestRequestGate();

  const setStatus = (message, state = '') => {
    status.textContent = message;
    status.dataset.state = state;
  };

  const setBusy = (next) => {
    busy = next;
    loadButton.disabled = next;
    loadButton.textContent = next ? 'Loading...' : 'Load my cosmetics';
  };

  const loadPublicCatalog = async () => {
    const version = requestGate.next();
    try {
      const response = await fetch(apiPath('/cosmetics/catalog'), {headers: {Accept: 'application/json'}});
      const data = await readJSON(response);
      if (!requestGate.isCurrent(version)) return;
      renderCatalog(catalogRoot, Array.isArray(data.items) ? data.items : [], false, () => {});
      if (checkoutState) {
        checkoutState.textContent = data.checkout_enabled ? 'Checkout enabled' : 'Preview catalog · checkout not yet enabled';
      }
    } catch (error) {
      if (!requestGate.isCurrent(version)) return;
      setStatus(`Catalog unavailable: ${error.message}`, 'error');
    }
  };

  const equip = async (item, button) => {
    if (!activeKey || busy) return;
    const version = requestGate.next();
    const key = activeKey;
    setBusy(true);
    button.disabled = true;
    button.textContent = 'Equipping...';
    try {
      const response = await fetch(apiPath('/bot/cosmetics'), {
        method: 'PUT',
        headers: {'X-Arena-Key': key, 'Content-Type': 'application/json'},
        body: JSON.stringify({slot: item.slot, cosmetic_id: item.id}),
      });
      await readJSON(response);
      if (!requestGate.isCurrent(version)) return;
      setStatus(`${item.name} equipped. Spectators will see it on the next state update.`, 'success');
      await loadInventory();
    } catch (error) {
      if (!requestGate.isCurrent(version)) return;
      setStatus(`Equip failed: ${error.message}`, 'error');
      button.disabled = false;
      button.textContent = 'Equip';
      setBusy(false);
    }
  };

  const loadInventory = async () => {
    const candidate = keyInput.value.trim();
    if (!candidate) {
      setStatus('Paste a bot API key to view ownership and equip items.', 'error');
      return;
    }
    const version = requestGate.next();
    setBusy(true);
    try {
      const response = await fetch(apiPath('/bot/cosmetics'), {
        headers: {'X-Arena-Key': candidate, Accept: 'application/json'},
        cache: 'no-store',
      });
      const data = await readJSON(response);
      if (!requestGate.isCurrent(version)) return;
      activeKey = candidate;
      renderCatalog(catalogRoot, Array.isArray(data.items) ? data.items : [], true, equip);
      setStatus('Inventory loaded. Free starters and owned items can be equipped now.', 'success');
    } catch (error) {
      if (!requestGate.isCurrent(version)) return;
      activeKey = '';
      setStatus(`Could not load bot cosmetics: ${error.message}`, 'error');
    } finally {
      if (requestGate.isCurrent(version)) setBusy(false);
    }
  };

  loadButton.addEventListener('click', loadInventory);
  keyInput.addEventListener('keydown', event => {
    if (event.key === 'Enter') loadInventory();
  });
  const stopListeningForClear = onArenaAPIKeyClear(() => {
    requestGate.invalidate();
    activeKey = '';
    keyInput.value = '';
    setBusy(false);
    setStatus('API key cleared from this page.', '');
    loadPublicCatalog();
  });
  if (clearButton) {
    clearButton.addEventListener('click', () => requestArenaAPIKeyClear());
  }

  loadPublicCatalog();
  return {
    setKey(key, load = true) {
      keyInput.value = key || '';
      if (load && key) loadInventory();
    },
    reload: loadInventory,
    clear: requestArenaAPIKeyClear,
    destroy: stopListeningForClear,
  };
}
