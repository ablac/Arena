'use strict';

import { apiPath } from './paths.js?v=20260710a';

const PAGE_SIZE = 12;

function formatPrice(item) {
  if (item.is_free) return 'Free';
  const rawCents = Number(item.price_cents || 0);
  const cents = Number.isFinite(rawCents) && rawCents >= 0 ? Math.round(rawCents) : 0;
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

function swatchForPack(pack) {
  const firstItem = Array.isArray(pack.items) ? pack.items[0] : null;
  const assetKey = firstItem?.asset_key || '';
  const helper = typeof window !== 'undefined' ? window.ArenaCosmeticThemes : globalThis.ArenaCosmeticThemes;
  return helper && typeof helper.swatchStyle === 'function' ? helper.swatchStyle(assetKey) : '';
}

function packSearchText(pack) {
  const items = Array.isArray(pack.items) ? pack.items : [];
  return [
    pack.id,
    pack.name,
    pack.description,
    ...items.flatMap(item => [item.id, item.name, item.description, item.rarity]),
  ].filter(Boolean).join(' ').toLowerCase();
}

function packCard(pack, checkoutEnabled) {
  const card = document.createElement('article');
  card.className = 'cosmetic-pack';
  card.dataset.cosmeticPack = pack.id || '';

  const swatch = document.createElement('div');
  swatch.className = 'cosmetic-pack-swatch';
  swatch.setAttribute('aria-hidden', 'true');
  const swatchStyle = swatchForPack(pack);
  if (swatchStyle) swatch.style.background = swatchStyle;

  const summary = document.createElement('div');
  summary.className = 'cosmetic-pack-summary';
  const name = document.createElement('strong');
  name.textContent = pack.name || pack.id || 'Cosmetic Set';
  const description = document.createElement('p');
  description.textContent = pack.description || 'A coordinated set of presentation-only Arena cosmetics.';
  summary.append(name, description);

  const contents = document.createElement('div');
  contents.className = 'cosmetic-pack-contents';
  const items = Array.isArray(pack.items) ? pack.items : [];
  if (items.length > 0) {
    for (const item of items.slice(0, 3)) {
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
  price.textContent = pack.is_free ? 'Free' : formatPrice(pack);
  const saleReady = checkoutEnabled && pack.is_purchasable === true;
  const state = document.createElement('span');
  state.className = 'cosmetic-state';
  state.textContent = pack.is_free ? 'Starter set' : (saleReady ? 'Ready to purchase' : 'Coming soon');
  offer.append(price, state);
  if (saleReady) {
    const link = document.createElement('a');
    link.className = 'btn btn-gold cosmetic-purchase-link';
    link.dataset.cosmeticPurchaseLink = pack.id || '';
    link.href = `dashboard/?tab=cosmetics&pack=${encodeURIComponent(pack.id || '')}`;
    link.textContent = 'Buy in Dashboard';
    offer.appendChild(link);
  }

  card.append(swatch, summary, contents, offer);
  return card;
}

function emptyState(query) {
  const empty = document.createElement('div');
  empty.className = 'cosmetics-empty-state';
  const title = document.createElement('strong');
  title.textContent = query ? 'No cosmetic sets match' : 'No cosmetic sets available';
  const copy = document.createElement('p');
  copy.textContent = query
    ? 'Try a set name, theme, item, or a different collection.'
    : 'The catalog is being prepared. Check back after the next Arena drop.';
  empty.append(title, copy);
  return empty;
}

function renderPacks(root, packs, checkoutEnabled, limit, query) {
  root.replaceChildren();
  if (packs.length === 0) {
    root.appendChild(emptyState(query));
    return 0;
  }
  const section = document.createElement('section');
  section.className = 'cosmetic-slot cosmetic-pack-section';
  const packList = document.createElement('div');
  packList.className = 'cosmetic-pack-list';
  const visible = packs.slice(0, limit);
  for (const pack of visible) packList.appendChild(packCard(pack, checkoutEnabled));
  section.appendChild(packList);
  root.appendChild(section);
  return visible.length;
}

function populateCategories(select, catalog, selected) {
  if (!select) return;
  const packCategoryIDs = new Set((catalog.packs || []).map(pack => pack.category_id).filter(Boolean));
  const categories = (catalog.categories || []).filter(category => packCategoryIDs.has(category.id));
  const options = [];
  const all = document.createElement('option');
  all.value = 'all';
  all.textContent = 'All collections';
  options.push(all);
  for (const category of categories) {
    const option = document.createElement('option');
    option.value = category.id;
    option.textContent = category.name || category.id;
    options.push(option);
  }
  select.replaceChildren(...options);
  select.value = categories.some(category => category.id === selected) ? selected : 'all';
}

/** Mount the public, pack-first cosmetic catalog. */
export function initCosmeticsPanel(container) {
  if (!container) return null;
  const status = container.querySelector('[data-cosmetic-status]');
  const catalogRoot = container.querySelector('[data-cosmetic-catalog]');
  const checkoutState = container.querySelector('[data-cosmetic-checkout]');
  const searchInput = container.querySelector('[data-cosmetic-search]');
  const categorySelect = container.querySelector('[data-cosmetic-category]');
  const resultsSummary = container.querySelector('[data-cosmetic-results-summary]');
  const showMoreButton = container.querySelector('[data-cosmetic-show-more]');
  if (!status || !catalogRoot) return null;

  const state = {
    catalog: {categories: [], packs: [], items: []},
    checkoutEnabled: false,
    query: '',
    category: 'all',
    visible: PAGE_SIZE,
    filteredCount: 0,
    renderedCount: 0,
  };

  const filteredPacks = () => {
    const query = state.query.trim().toLowerCase();
    return (state.catalog.packs || []).filter(pack => {
      if (state.category !== 'all' && pack.category_id !== state.category) return false;
      return !query || packSearchText(pack).includes(query);
    });
  };

  const render = () => {
    const packs = filteredPacks();
    state.filteredCount = packs.length;
    state.renderedCount = renderPacks(catalogRoot, packs, state.checkoutEnabled, state.visible, state.query);
    if (resultsSummary) {
      resultsSummary.textContent = packs.length
        ? `Showing ${state.renderedCount} of ${packs.length} cosmetic sets`
        : 'No cosmetic sets found';
    }
    if (showMoreButton) {
      showMoreButton.hidden = state.renderedCount >= packs.length;
      showMoreButton.textContent = `Show ${Math.min(PAGE_SIZE, Math.max(0, packs.length - state.renderedCount))} more sets`;
    }
  };

  const setQuery = value => {
    state.query = String(value || '').trim();
    state.visible = PAGE_SIZE;
    if (searchInput && searchInput.value !== state.query) searchInput.value = state.query;
    render();
  };

  const setCategory = value => {
    state.category = String(value || 'all');
    state.visible = PAGE_SIZE;
    if (categorySelect && categorySelect.value !== state.category) categorySelect.value = state.category;
    render();
  };

  const showMore = () => {
    state.visible += PAGE_SIZE;
    render();
  };

  const loadCatalog = async () => {
    status.textContent = 'Loading cosmetic sets...';
    status.dataset.state = 'loading';
    try {
      const response = await fetch(apiPath('/cosmetics/catalog'), {
        headers: {Accept: 'application/json'},
        cache: 'no-store',
      });
      const data = await readJSON(response);
      state.catalog = {
        categories: Array.isArray(data.categories) ? data.categories : [],
        packs: Array.isArray(data.packs) ? data.packs : [],
        items: Array.isArray(data.items) ? data.items : [],
      };
      state.checkoutEnabled = data.checkout_enabled === true;
      populateCategories(categorySelect, state.catalog, state.category);
      state.category = categorySelect?.value || 'all';
      state.visible = PAGE_SIZE;
      render();
      if (checkoutState) {
        checkoutState.textContent = state.checkoutEnabled
          ? 'Purchases open in Bot Dashboard'
          : 'Preview catalog · checkout not yet enabled';
      }
      status.textContent = 'Purchases belong to your verified email account. Each set can be assigned across linked bots one license at a time.';
      status.dataset.state = 'success';
    } catch (error) {
      state.filteredCount = 0;
      state.renderedCount = 0;
      catalogRoot.replaceChildren();
      const errorState = document.createElement('div');
      errorState.className = 'cosmetics-empty-state cosmetics-error-state';
      const message = document.createElement('p');
      message.textContent = `Catalog unavailable: ${error.message}`;
      const retry = document.createElement('button');
      retry.type = 'button';
      retry.textContent = 'Retry catalog';
      retry.addEventListener('click', loadCatalog);
      errorState.append(message, retry);
      catalogRoot.appendChild(errorState);
      status.textContent = 'The cosmetic catalog could not be loaded. Retry when the Arena connection is available.';
      status.dataset.state = 'error';
      if (showMoreButton) showMoreButton.hidden = true;
    }
  };

  searchInput?.addEventListener('input', event => setQuery(event.currentTarget.value));
  categorySelect?.addEventListener('change', event => setCategory(event.currentTarget.value));
  showMoreButton?.addEventListener('click', showMore);

  loadCatalog();
  return {
    reload: loadCatalog,
    setCategory,
    setQuery,
    showMore,
    snapshot: () => ({
      filteredCount: state.filteredCount,
      renderedCount: state.renderedCount,
      query: state.query,
      category: state.category,
    }),
  };
}
