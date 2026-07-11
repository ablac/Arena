'use strict';

import { apiPath } from './paths.js?v=20260710a';

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

function itemCard(item, checkoutEnabled) {
  const card = document.createElement('article');
  card.className = `cosmetic-item rarity-${item.rarity || 'common'}`;

  const header = document.createElement('div');
  header.className = 'cosmetic-item-header';
  const name = document.createElement('strong');
  name.textContent = item.name || item.id || 'Cosmetic';
  const price = document.createElement('span');
  price.className = 'cosmetic-price';
  const saleReady = checkoutEnabled && item.is_purchasable === true;
  price.textContent = item.is_free ? 'Free' : (saleReady ? formatPrice(item) : 'Preview');
  header.append(name, price);

  const description = document.createElement('p');
  description.textContent = item.description || 'Presentation-only Arena customization.';

  const footer = document.createElement('div');
  footer.className = 'cosmetic-item-footer';
  const badge = document.createElement('span');
  badge.className = 'cosmetic-state';
  badge.textContent = item.is_free ? 'Starter item' : (saleReady ? 'Email-account license' : 'Coming soon');
  footer.append(badge);

  card.append(header, description, footer);
  return card;
}

function packCard(pack, checkoutEnabled) {
  const card = document.createElement('article');
  card.className = 'cosmetic-pack';

  const summary = document.createElement('div');
  summary.className = 'cosmetic-pack-summary';
  const name = document.createElement('strong');
  name.textContent = pack.name || pack.id || 'Cosmetic Pack';
  const description = document.createElement('p');
  description.textContent = pack.description || 'A coordinated set of presentation-only Arena cosmetics.';
  summary.append(name, description);

  const contents = document.createElement('div');
  contents.className = 'cosmetic-pack-contents';
  const items = Array.isArray(pack.items) ? pack.items : [];
  if (items.length > 0) {
    for (const item of items) {
      const label = document.createElement('span');
      label.textContent = item.name || item.id || 'Cosmetic';
      contents.appendChild(label);
    }
  } else {
    const count = Array.isArray(pack.item_ids) ? pack.item_ids.length : 0;
    const label = document.createElement('span');
    label.textContent = count > 0 ? `${count} cosmetics` : 'Contents being prepared';
    contents.appendChild(label);
  }

  const offer = document.createElement('div');
  offer.className = 'cosmetic-pack-offer';
  const price = document.createElement('span');
  price.className = 'cosmetic-pack-price';
  const saleReady = checkoutEnabled && pack.is_purchasable === true;
  price.textContent = pack.is_free ? 'Free' : (saleReady ? formatPrice(pack) : `Preview · ${formatPrice(pack)}`);
  const state = document.createElement('span');
  state.className = 'cosmetic-state';
  state.textContent = pack.is_free ? 'Starter pack' : (saleReady ? 'Available in Bot Dashboard' : 'Coming soon');
  offer.append(price, state);

  card.append(summary, contents, offer);
  return card;
}

function renderCatalog(root, catalog, checkoutEnabled) {
  root.replaceChildren();
  const packs = Array.isArray(catalog.packs) ? catalog.packs : [];
  const items = Array.isArray(catalog.items) ? catalog.items : [];

  if (packs.length > 0) {
    const packSection = document.createElement('section');
    packSection.className = 'cosmetic-slot cosmetic-pack-section';
    const heading = document.createElement('h4');
    heading.textContent = 'Curated Packs';
    const packList = document.createElement('div');
    packList.className = 'cosmetic-pack-list';
    for (const pack of packs) {
      packList.appendChild(packCard(pack, checkoutEnabled));
    }
    packSection.append(heading, packList);
    root.appendChild(packSection);
  }

  for (const [slot, label] of Object.entries(SLOT_LABELS)) {
    const slotItems = items.filter(candidate => candidate.slot === slot);
    if (slotItems.length === 0) continue;
    const section = document.createElement('section');
    section.className = 'cosmetic-slot';
    const heading = document.createElement('h4');
    heading.textContent = label;
    const grid = document.createElement('div');
    grid.className = 'cosmetic-items';
    for (const item of slotItems) {
      grid.appendChild(itemCard(item, checkoutEnabled));
    }
    section.append(heading, grid);
    root.appendChild(section);
  }
}

/**
 * Mount the public catalog preview. Durable ownership and bot assignment live
 * in the verified-email Bot Dashboard; this page never asks for an API key.
 */
export function initCosmeticsPanel(container) {
  if (!container) return null;
  const status = container.querySelector('[data-cosmetic-status]');
  const catalogRoot = container.querySelector('[data-cosmetic-catalog]');
  const checkoutState = container.querySelector('[data-cosmetic-checkout]');
  if (!status || !catalogRoot) return null;

  const loadCatalog = async () => {
    try {
      const response = await fetch(apiPath('/cosmetics/catalog'), {
        headers: {Accept: 'application/json'},
        cache: 'no-store',
      });
      const data = await readJSON(response);
      renderCatalog(catalogRoot, data, data.checkout_enabled === true);
      if (checkoutState) {
        checkoutState.textContent = data.checkout_enabled
          ? 'Checkout enabled'
          : 'Preview catalog · checkout not yet enabled';
      }
      status.textContent = 'Purchases belong to your verified email account. Assign each license to one linked bot at a time in the Bot Dashboard.';
      status.dataset.state = 'success';
    } catch (error) {
      status.textContent = `Catalog unavailable: ${error.message}`;
      status.dataset.state = 'error';
    }
  };

  loadCatalog();
  return {reload: loadCatalog};
}
