import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

class FakeStyle {
  constructor() { this.values = new Map(); }
  setProperty(name, value) { this.values.set(name, value); }
}

class FakeElement {
  constructor(tagName = 'div') {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.dataset = {};
    this.className = '';
    this.textContent = '';
    this.value = '';
    this.hidden = false;
    this.href = '';
    this.style = new FakeStyle();
    this.listeners = new Map();
  }
  append(...nodes) { this.children.push(...nodes); }
  appendChild(node) { this.children.push(node); return node; }
  replaceChildren(...nodes) { this.children = [...nodes]; }
  addEventListener(name, listener) { this.listeners.set(name, listener); }
  setAttribute(name, value) { this[name] = String(value); }
  querySelector() { return null; }
}

class FakeContainer extends FakeElement {
  constructor(elements) {
    super('section');
    this.elements = elements;
  }
  querySelector(selector) { return this.elements[selector] || null; }
}

function findNodes(root, predicate, matches = []) {
  if (predicate(root)) matches.push(root);
  for (const child of root.children || []) findNodes(child, predicate, matches);
  return matches;
}

function syntheticCatalog(checkoutEnabled = false) {
  const categories = [
    {id: 'season-alpha', name: 'Season Alpha', is_active: true, sort_order: 10},
    {id: 'season-beta', name: 'Season Beta', is_active: true, sort_order: 20},
  ];
  const items = [];
  const packs = [];
  for (let index = 1; index <= 100; index += 1) {
    const number = String(index).padStart(3, '0');
    const categoryID = index <= 50 ? 'season-alpha' : 'season-beta';
    const assetKey = `arena_set_${number}_signal_${number}`;
    const setItems = ['bot_skin', 'weapon_skin', 'attachment'].map((slot, itemIndex) => {
      const item = {
        id: `${slot}-${number}`,
        name: `Set ${number} ${slot}`,
        description: `Visual ${slot} for set ${number}`,
        category_id: categoryID,
        slot,
        asset_key: assetKey,
        rarity: 'rare',
        price_cents: 199,
        currency: 'USD',
        is_purchasable: true,
      };
      items.push(item);
      return {...item, sort_order: (itemIndex + 1) * 10};
    });
    packs.push({
      id: `set-${number}-pack`,
      name: `Signal Set ${number}`,
      description: `Coordinated Arena set ${number}`,
      category_id: categoryID,
      price_cents: 499,
      currency: 'USD',
      is_purchasable: true,
      items: setItems,
    });
  }
  return {checkout_enabled: checkoutEnabled, categories, items, packs};
}

globalThis.document = {createElement: tagName => new FakeElement(tagName)};
globalThis.ArenaCosmeticThemes = {
  swatchStyle: key => key.startsWith('arena_set_') ? 'linear-gradient(135deg, #112233, #44aacc)' : '',
};

let panelSource = readFileSync(new URL('../frontend/js/cosmetics-panel.js', import.meta.url), 'utf8');
panelSource = panelSource.replace(/import \{ apiPath \} from '[^']+';\r?\n/, "const apiPath = path => '/api/v1' + path;\n");
const {initCosmeticsPanel} = await import(dataModule(panelSource));

const status = new FakeElement('p');
const catalogRoot = new FakeElement('div');
const checkoutState = new FakeElement('span');
const search = new FakeElement('input');
const category = new FakeElement('select');
const summary = new FakeElement('p');
const showMore = new FakeElement('button');
const panel = new FakeContainer({
  '[data-cosmetic-status]': status,
  '[data-cosmetic-catalog]': catalogRoot,
  '[data-cosmetic-checkout]': checkoutState,
  '[data-cosmetic-search]': search,
  '[data-cosmetic-category]': category,
  '[data-cosmetic-results-summary]': summary,
  '[data-cosmetic-show-more]': showMore,
});

let responseData = syntheticCatalog(false);
globalThis.fetch = async () => ({ok: true, json: async () => responseData});

const api = initCosmeticsPanel(panel);
await new Promise(resolve => setTimeout(resolve, 0));

assert.deepEqual(api.snapshot(), {filteredCount: 100, renderedCount: 12, query: '', category: 'all'});
assert.equal(findNodes(catalogRoot, node => node.className === 'cosmetic-pack').length, 12, 'initial render must be bounded to 12 packs');
assert.equal(findNodes(catalogRoot, node => node.className === 'cosmetic-item').length, 0, 'the 300 item rows must not render by default');
assert.equal(findNodes(catalogRoot, node => node.dataset?.cosmeticPurchaseLink).length, 0, 'checkout-disabled catalogs must not show purchase links');
assert.equal(showMore.hidden, false);

api.setQuery('Signal Set 099');
assert.deepEqual(api.snapshot(), {filteredCount: 1, renderedCount: 1, query: 'Signal Set 099', category: 'all'});
assert.ok(findNodes(catalogRoot, node => node.textContent === 'Signal Set 099').length, 'search should reveal the matching pack');

api.setQuery('not-a-real-set');
assert.equal(api.snapshot().filteredCount, 0);
assert.ok(findNodes(catalogRoot, node => node.className === 'cosmetics-empty-state').length, 'zero search results need a dedicated state');

api.setQuery('');
api.setCategory('season-beta');
assert.equal(api.snapshot().filteredCount, 50, 'category filtering should use managed pack categories');
api.showMore();
assert.equal(api.snapshot().renderedCount, 24, 'show more should reveal one bounded page at a time');

responseData = syntheticCatalog(true);
await api.reload();
assert.equal(findNodes(catalogRoot, node => node.dataset?.cosmeticPurchaseLink).length, 12, 'sale-ready packs should link to Dashboard only when checkout is enabled');

const desktopHTML = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
assert.ok(desktopHTML.indexOf('js/cosmetic-themes.js') < desktopHTML.indexOf('js/app.js'), 'desktop must load themes before the renderer module chain');
const mobileHTML = readFileSync(new URL('../frontend/m/index.html', import.meta.url), 'utf8');
assert.ok(mobileHTML.indexOf('../js/cosmetic-themes.js') < mobileHTML.indexOf('mobile.js'), 'mobile must load themes before its shared renderer');
assert.match(mobileHTML, /data-mobile-cosmetic-shop/, 'mobile spectator needs a direct Dashboard shop link');
const appSource = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
const mobileSource = readFileSync(new URL('../frontend/m/mobile.js', import.meta.url), 'utf8');
const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');
const botsSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
assert.match(appSource, /cosmetics-panel\.js\?v=20260711b/, 'desktop must invalidate the public catalog module cache');
assert.match(appSource, /renderer\/engine\.js\?v=20260711b/, 'desktop must invalidate the renderer engine cache');
assert.match(mobileSource, /renderer\/engine\.js\?v=20260711b/, 'mobile must invalidate the renderer engine cache');
assert.match(engineSource, /bots\.js\?v=20260711b/, 'the engine must invalidate the bot renderer cache');
assert.match(botsSource, /cosmetics\.js\?v=20260711b/, 'the bot renderer must invalidate the cosmetic renderer cache');

console.log('100-pack storefront stays bounded, searchable, filterable, and checkout-gated');
