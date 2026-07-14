import assert from 'node:assert/strict';
import {existsSync, readFileSync} from 'node:fs';

const shopHTMLURL = new URL('../frontend/shop/index.html', import.meta.url);
const shopModuleURL = new URL('../frontend/js/cosmetics-shop.js', import.meta.url);
const shopCSSURL = new URL('../frontend/css/shop.css', import.meta.url);

assert.equal(existsSync(shopHTMLURL), true, 'cosmetics need a dedicated /shop/ document');
assert.equal(existsSync(shopModuleURL), true, 'the Shop needs an isolated catalog controller');

const shopHTML = readFileSync(shopHTMLURL, 'utf8');
const shopCSS = readFileSync(shopCSSURL, 'utf8');
const mainHTML = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const shopIDs = Array.from(shopHTML.matchAll(/\sid="([^"]+)"/g), match => match[1]);
assert.equal(shopIDs.length, new Set(shopIDs).size, 'Shop document must not contain duplicate IDs');
for (const match of shopHTML.matchAll(/aria-labelledby="([^"]+)"/g)) {
  for (const id of match[1].split(/\s+/)) {
    assert.ok(shopIDs.includes(id), `Shop aria-labelledby must resolve #${id}`);
  }
}

assert.match(shopHTML, /<main[^>]*id="cosmetic-shop"[^>]*tabindex="-1"/,
  'the skip-link target must accept programmatic focus without joining the normal tab order');
assert.match(shopHTML, /<script defer src="https:\/\/cdn\.babylonjs\.com\/v9\.14\.0\/babylon\.js"><\/script>/,
  'Babylon core must not block parsing the Shop body');
assert.doesNotMatch(shopHTML, /materialsLibrary/, 'Shop must not load the unused Babylon materials add-on');
assert.ok(shopHTML.indexOf('babylon.js') < shopHTML.indexOf('cosmetic-themes.js')
  && shopHTML.indexOf('cosmetic-themes.js') < shopHTML.indexOf('cosmetics-shop.js'),
  'Shop scripts must preserve renderer dependency order');
assert.equal((shopHTML.match(/<canvas\b/g) || []).length, 1, 'Shop must use one shared bot preview canvas');
assert.match(shopHTML, /id="shop-preview-canvas"/, 'the large bot preview canvas needs a stable hook');
assert.match(shopHTML, /data-shop-pack-list/, 'Shop needs a pack browser');
assert.match(shopHTML, /data-shop-kind/, 'Shop needs a product-type filter');
assert.match(shopHTML, /data-shop-kind[\s\S]{0,400}<option value="trails">Trails<\/option>/,
  'Trails must be a first-class filter even when catalog collection metadata changes');
assert.match(shopHTML, /data-shop-kind[\s\S]{0,500}<option value="body-forms">Full-body skins<\/option>/,
  'full-body skins must be a first-class product filter');
assert.match(shopHTML, /data-shop-sort/, 'Shop needs an explicit sort control');
assert.match(shopHTML, /data-shop-pack-detail/, 'Shop needs a selected-pack detail region');
assert.match(shopHTML, /data-shop-item-list/, 'pack detail must expose its complete item list');
assert.match(shopHTML, /data-shop-preview-pack/, 'customers need a full-pack preview control');
assert.match(shopHTML, /data-shop-rotate-left/, 'preview must have a non-gesture rotate-left control');
assert.match(shopHTML, /data-shop-rotate-right/, 'preview must have a non-gesture rotate-right control');
assert.match(shopHTML, /data-shop-reset-view/, 'preview must have a reset control');
assert.match(shopHTML, /data-shop-purchase[^>]*\shidden(?:\s|>)/,
  'purchase link must not be keyboard-operable before a purchasable pack loads');
assert.match(shopHTML, /Each purchased item copy can be assigned to one bot at a time/i,
  'Shop must state the per-item license rule precisely');
assert.match(shopHTML, /Items from the same pack can be assigned to different bots/i,
  'Shop must explain that pack members are independent licenses');
assert.match(shopHTML, /data-shop-subscription/, 'Shop needs one prominent horizontal All Access offer');
assert.match(shopHTML, /All Access/);
assert.match(shopHTML, /every current and future cosmetic set, full-body skin, and trail/i);
assert.match(shopHTML, /up to 5 active API keys/i);
assert.match(shopHTML, /subscription cosmetics are removed/i);
assert.match(shopHTML, /data-shop-subscription-action/, 'All Access must lead into the verified-email Dashboard');
assert.match(shopHTML, /data-shop-subscription-action[^>]*\shidden(?:\s|>)/,
  'All Access action must not be operable before checkout availability is confirmed');
assert.match(shopCSS, /\.shop-all-access-offer \[hidden\]\s*\{[^}]*display:\s*none\s*!important/s,
  'author button styles must not override the unavailable offer hidden state');
assert.match(mainHTML, /data-overlay-open="shop-overlay"[^>]*>[\s\S]*?<span>Shop<\/span>/,
  'the main command dock must open the Shop as a slide-out drawer');
assert.match(mainHTML, /class="mobile-command-actions"[\s\S]*?data-overlay-open="shop-overlay"[^>]*>Shop<\/button>/,
  'mobile quick actions must open the dedicated Shop drawer directly');
assert.match(mainHTML, /id="shop-overlay"[\s\S]*?data-src="\/shop\/[^"]*"/,
  'the Shop drawer must lazy-load the dedicated Shop document');

let source = readFileSync(shopModuleURL, 'utf8');
assert.match(source, /dataset\.shopPackId\s*=/, 'pack hooks must serialize as data-shop-pack-id in real DOM');
assert.match(source, /dataset\.shopItemId\s*=/, 'item hooks must serialize as data-shop-item-id in real DOM');
assert.doesNotMatch(source, /dataset\.shop(?:Pack|Item)ID\s*=/, 'dataset acronyms must not split into data-*-i-d attributes');
source = source.replace(/import \{[^}]*\} from '\.\/paths\.js[^']*';\r?\n/, `
  const appPath = (path, pathname = '/') =>
    (pathname === '/arena' || pathname.startsWith('/arena/')) ? '/arena' + path : path;
  const apiPath = (path, pathname = '/') =>
    (pathname === '/arena' || pathname.startsWith('/arena/')) ? '/arena/api/v1' + path : '/api/v1' + path;
`);
source = source.replace(/import \{ CosmeticShopPreview \} from '\.\/shop-preview\.js[^']*';\r?\n/,
  'class CosmeticShopPreview {}\n');
const shop = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

const pack = {
  id: 'ember-pack',
  items: [
    {id: 'body-first', slot: 'bot_skin', asset_key: 'arena_set_003_ember'},
    {id: 'body-alt', slot: 'bot_skin', asset_key: 'arena_set_004_alt'},
    {id: 'weapon', slot: 'weapon_skin', asset_key: 'arena_set_003_ember'},
    {id: 'attachment', slot: 'attachment', asset_key: 'arena_set_003_ember'},
    {id: 'trail', slot: 'trail', asset_key: 'ember_sparks'},
  ],
};

const trailPack = {
  id: 'trail-only', name: 'Comet Tail', category_id: 'trails', price_cents: 99,
  items: [{id: 'trail-item', name: 'Comet Tail', slot: 'trail', asset_key: 'comet_tail'}],
};
const bodyFormPack = {
  id: 'giant-chicken-pack', name: 'Giant Chicken', category_id: 'body-forms', price_cents: 199,
  items: [{id: 'giant-chicken', name: 'Giant Chicken', slot: 'bot_skin', asset_key: 'body_giant_chicken'}],
};
assert.equal(shop.isTrailPack(trailPack), true);
assert.equal(shop.isTrailPack(pack), false, 'a coordinated set containing a trail remains a set');
assert.equal(shop.isBodyFormPack(bodyFormPack), true);
assert.equal(shop.isBodyFormPack(pack), false, 'a coordinated chassis set is not a full-body skin');
assert.deepEqual(
  shop.sortCosmeticPacks([
    {id: 'zulu', name: 'Zulu', price_cents: 99},
    {id: 'alpha', name: 'Alpha', price_cents: 199},
  ], 'name').map(candidate => candidate.id),
  ['alpha', 'zulu'],
  'name sorting must be deterministic and must not mutate the catalog order',
);

assert.deepEqual(shop.packPreviewLoadout(pack), {
  bot_skin: 'arena_set_003_ember',
  weapon_skin: 'arena_set_003_ember',
  attachment: 'arena_set_003_ember',
  trail: 'ember_sparks',
}, 'full-pack preview must use the first ordered item in every supported slot');
assert.deepEqual(shop.itemPreviewLoadout(pack.items[2]), {
  bot_skin: 'standard',
  weapon_skin: 'arena_set_003_ember',
  attachment: 'none',
  trail: 'standard',
}, 'individual preview must isolate the chosen item against standard defaults');
assert.deepEqual(shop.itemPreviewLoadout(pack.items[4]), {
  bot_skin: 'standard',
  weapon_skin: 'standard',
  attachment: 'none',
  trail: 'ember_sparks',
}, 'individual trail preview must isolate the selected trail against standard defaults');
assert.deepEqual(shop.packItems(pack).map(item => item.id), ['body-first', 'body-alt', 'weapon', 'attachment', 'trail'],
  'pack detail must preserve every catalog item, including multiple items in one slot');
assert.equal(shop.dashboardPurchasePath('ember pack', '/shop/'), '/?dash_open=1&dash_tab=cosmetics');
assert.equal(shop.dashboardPurchasePath('ember pack', '/arena/shop/'), '/arena/?dash_open=1&dash_tab=cosmetics');
assert.equal(shop.subscriptionDashboardPath('/shop/'), '/?dash_open=1&dash_tab=cosmetics&dash_plan=all-access');
assert.equal(shop.subscriptionDashboardPath('/arena/shop/'), '/arena/?dash_open=1&dash_tab=cosmetics&dash_plan=all-access');
assert.equal(shop.catalogPath('/arena/shop/'), '/arena/api/v1/cosmetics/catalog');

class FakeStyle {
  constructor() { this.background = ''; }
}

class FakeElement {
  constructor(tagName = 'div') {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.dataset = {};
    this.attributes = new Map();
    this.listeners = new Map();
    this.className = '';
    this.textContent = '';
    this.value = '';
    this.hidden = false;
    this.disabled = false;
    this.href = '';
    this.style = new FakeStyle();
  }
  append(...nodes) { this.children.push(...nodes); }
  appendChild(node) { this.children.push(node); return node; }
  replaceChildren(...nodes) {
    const active = globalThis.document?.activeElement;
    if (this.children.includes(active) && !nodes.includes(active)) {
      globalThis.document.activeElement = globalThis.document.body;
    }
    this.children = [...nodes];
  }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  addEventListener(name, listener) { this.listeners.set(name, listener); }
  removeEventListener(name, listener) {
    if (this.listeners.get(name) === listener) this.listeners.delete(name);
  }
  click() {
    globalThis.document.activeElement = this;
    this.listeners.get('click')?.({currentTarget: this});
  }
  closest() { return null; }
}

class FakeRoot extends FakeElement {
  constructor(elements) { super('main'); this.elements = elements; }
  querySelector(selector) { return this.elements[selector] || null; }
}

const canvas = new FakeElement('canvas');
const status = new FakeElement('p');
const packList = new FakeElement('div');
const detail = new FakeElement('aside');
const itemList = new FakeElement('div');
const search = new FakeElement('input');
const category = new FakeElement('select');
const kind = new FakeElement('select');
const sort = new FakeElement('select');
const summary = new FakeElement('p');
const showMore = new FakeElement('button');
const packName = new FakeElement('h2');
const packDescription = new FakeElement('p');
const packPrice = new FakeElement('strong');
const packCount = new FakeElement('p');
const purchase = new FakeElement('a');
const previewPack = new FakeElement('button');
const previewLabel = new FakeElement('p');
const previewStatus = new FakeElement('p');
const rotateLeft = new FakeElement('button');
const rotateRight = new FakeElement('button');
const resetView = new FakeElement('button');
const subscription = new FakeElement('section');
const subscriptionPrice = new FakeElement('strong');
const subscriptionAction = new FakeElement('a');
const subscriptionState = new FakeElement('p');
const root = new FakeRoot({
  '#shop-preview-canvas': canvas,
  '[data-shop-status]': status,
  '[data-shop-search]': search,
  '[data-shop-category]': category,
  '[data-shop-kind]': kind,
  '[data-shop-sort]': sort,
  '[data-shop-results-summary]': summary,
  '[data-shop-show-more]': showMore,
  '[data-shop-pack-list]': packList,
  '[data-shop-pack-detail]': detail,
  '[data-shop-item-list]': itemList,
  '[data-shop-pack-name]': packName,
  '[data-shop-pack-description]': packDescription,
  '[data-shop-pack-price]': packPrice,
  '[data-shop-pack-count]': packCount,
  '[data-shop-purchase]': purchase,
  '[data-shop-preview-pack]': previewPack,
  '[data-shop-preview-label]': previewLabel,
  '[data-shop-preview-status]': previewStatus,
  '[data-shop-rotate-left]': rotateLeft,
  '[data-shop-rotate-right]': rotateRight,
  '[data-shop-reset-view]': resetView,
  '[data-shop-subscription]': subscription,
  '[data-shop-subscription-price]': subscriptionPrice,
  '[data-shop-subscription-action]': subscriptionAction,
  '[data-shop-subscription-state]': subscriptionState,
});

globalThis.document = {
  activeElement: null,
  body: new FakeElement('body'),
  createElement: tagName => new FakeElement(tagName),
};
globalThis.window = Object.assign(new FakeElement('window'), {
  location: {pathname: '/shop/', search: '', href: 'https://arena.example/shop/'},
  ArenaCosmeticThemes: {swatchStyle: () => 'linear-gradient(#000, #fff)'},
  matchMedia: () => ({matches: false}),
});

const previewCalls = [];
const fakePreview = {
  init() { previewCalls.push({type: 'init'}); return this; },
  setLoadout(loadout) { previewCalls.push({type: 'loadout', loadout}); },
  rotateBy() {},
  resetRotation() {},
  dispose() {},
};
const bulkPacks = Array.from({length: 99}, (_, index) => {
  const number = String(index + 2).padStart(3, '0');
  const assetKey = `arena_set_${number}_signal_test`;
  const finalSet = number === '100';
  return {
    id: finalSet ? 'arena-set-100-apex-radiance-pack' : `signal-set-${number}`,
    name: finalSet ? 'Apex Radiance Set' : `Signal Set ${number}`,
    description: finalSet ? 'A coordinated three-piece Apex Radiance cosmetic set' : `Coordinated Arena set ${number}`,
    category_id: 'season-one',
    price_cents: 199,
    currency: 'USD',
    is_purchasable: true,
    items: [{
      id: `chassis-${number}`,
      name: `Signal ${number} Chassis`,
      slot: 'bot_skin',
      asset_key: assetKey,
    }],
  };
});
const catalog = {
  checkout_enabled: true,
  subscription_offer: {
    enabled: true,
    price_cents: 1999,
    currency: 'USD',
    interval: 'month',
    includes_future_sets: true,
    max_api_keys: 5,
  },
  categories: [{id: 'season-one', name: 'Season One'}, {id: 'body-forms', name: 'Body Forms'}],
  packs: [
    {...pack, name: 'Ember Pack', category_id: 'season-one', price_cents: 199, currency: 'USD', is_purchasable: true},
    {...trailPack, currency: 'USD', is_purchasable: true},
    {...bodyFormPack, currency: 'USD', is_purchasable: true},
    ...bulkPacks,
  ],
};
let resolveCatalog;
const catalogReady = new Promise(resolve => { resolveCatalog = resolve; });
const controller = shop.initCosmeticsShop(root, {
  pathname: '/shop/',
  requestedPackID: 'ember-pack',
  updateURL: false,
  previewFactory: () => fakePreview,
  fetchImpl: async () => ({ok: true, json: async () => catalogReady}),
});
search.value = 'Signal Set 099';
search.listeners.get('input')({currentTarget: search});
resolveCatalog(catalog);
await new Promise(resolve => setTimeout(resolve, 0));

assert.equal(controller.snapshot().selectedPackID, 'signal-set-099',
  'a search typed during fetch must determine the selected pack when the response arrives');
assert.equal(packList.children.length, 1);
assert.equal(subscriptionPrice.textContent, '$19.99 / month');
assert.equal(subscriptionAction.href, '/?dash_open=1&dash_tab=cosmetics&dash_plan=all-access');
assert.equal(subscriptionAction.hidden, false);
assert.match(subscriptionState.textContent, /current and future/i);

category.value = 'season-one';
category.listeners.get('change')({currentTarget: category});
assert.equal(controller.snapshot().filteredCount, 1, 'collection filters should remain active before switching product families');
kind.value = 'trails';
kind.listeners.get('change')({currentTarget: kind});
assert.equal(controller.snapshot().filteredCount, 1, 'the Trails filter must isolate standalone trail products');
assert.equal(controller.snapshot().selectedPackID, 'trail-only');
assert.equal(packList.children[0].dataset.shopPackId, 'trail-only');
assert.equal(category.value, 'all', 'switching product families must clear an incompatible collection filter');

kind.value = 'body-forms';
kind.listeners.get('change')({currentTarget: kind});
assert.equal(controller.snapshot().filteredCount, 1, 'the full-body filter must isolate body-form products');
assert.equal(controller.snapshot().selectedPackID, 'giant-chicken-pack');
assert.equal(packList.children[0].dataset.shopPackId, 'giant-chicken-pack');
assert.match(packList.children[0].children[1].children[1].textContent, /Full-body skin/);

kind.value = 'all';
kind.listeners.get('change')({currentTarget: kind});
sort.value = 'name';
sort.listeners.get('change')({currentTarget: sort});
assert.equal(controller.snapshot().sort, 'name');
assert.equal(packList.children[0].dataset.shopPackId, 'arena-set-100-apex-radiance-pack',
  'changing sort order must visibly reorder the product browser and its active selection');

search.value = 'set 100';
search.listeners.get('input')({currentTarget: search});
assert.equal(controller.snapshot().filteredCount, 1, 'natural spacing must find a hyphenated set number');
assert.equal(controller.snapshot().selectedPackID, 'arena-set-100-apex-radiance-pack');

search.value = '';
search.listeners.get('input')({currentTarget: search});
assert.equal(packList.children.length, 24, 'the 100-pack Shop must keep its initial DOM page bounded');
assert.equal(packList.children[0].dataset.shopPackId, 'arena-set-100-apex-radiance-pack',
  'the selected pack must stay visible when a broader filter is restored');

const emberSortedButton = packList.children.find(button => button.dataset.shopPackId === 'ember-pack');
emberSortedButton.click();
search.value = '';
search.listeners.get('input')({currentTarget: search});
assert.equal(controller.snapshot().selectedPackID, 'ember-pack');
assert.equal(packList.children[0].dataset.shopPackId, 'arena-set-100-apex-radiance-pack',
  'preserving a selection must not move it ahead of the selected name/price sort');
showMore.listeners.get('click')();
assert.equal(packList.children.length, 48, 'Show more must reveal one bounded page at a time');

const emberButton = packList.children.find(button => button.dataset.shopPackId === 'ember-pack');
emberButton.click();
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(packList.children.find(button => button.dataset.shopPackId === 'ember-pack'), emberButton,
  'pack selection must update in place so keyboard focus is not discarded');
assert.equal(document.activeElement, emberButton, 'pack selection must retain keyboard focus');
assert.equal(controller.snapshot().selectedPackID, 'ember-pack');
assert.equal(itemList.children.length, 5, 'selecting a pack must render every item, including its trail');
assert.equal(packCount.textContent, '5 included items');
assert.equal(packPrice.textContent, '$1.99');
assert.equal(purchase.href, '/?dash_open=1&dash_tab=cosmetics');
assert.equal(purchase.hidden, false);
assert.deepEqual(previewCalls.at(-1).loadout, shop.packPreviewLoadout(pack));

const weaponButton = itemList.children[2];
weaponButton.click();
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(controller.snapshot().selectedItemID, 'weapon');
assert.equal(itemList.children[2], weaponButton,
  'item selection must update in place so keyboard focus is not discarded');
assert.equal(document.activeElement, weaponButton, 'item selection must retain keyboard focus');
assert.deepEqual(previewCalls.at(-1).loadout, shop.itemPreviewLoadout(pack.items[2]));
assert.equal(itemList.children[2].getAttribute('aria-pressed'), 'true', 'selected item must publish pressed state');

controller.previewPack();
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(controller.snapshot().selectedItemID, '');
assert.match(canvas.dataset.previewSignature, /full-pack$/);

search.value = 'no such cosmetic';
search.listeners.get('input')({currentTarget: search});
assert.equal(controller.snapshot().selectedPackID, '');
assert.equal(canvas.dataset.previewSignature, 'standard:no-pack-selected',
  'an empty filter must clear stale pack cosmetics from the preview');
assert.deepEqual(previewCalls.at(-1).loadout, {
  bot_skin: 'standard',
  weapon_skin: 'standard',
  attachment: 'none',
  trail: 'standard',
});
controller.dispose();

subscriptionAction.hidden = false;
subscriptionAction.attributes.clear();
const unavailableController = shop.initCosmeticsShop(root, {
  pathname: '/shop/',
  updateURL: false,
  previewFactory: () => fakePreview,
  fetchImpl: async () => ({
    ok: true,
    json: async () => ({
      ...catalog,
      subscription_offer: {...catalog.subscription_offer, enabled: false},
    }),
  }),
});
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(subscriptionAction.hidden, true,
  'an unavailable All Access checkout must not leave a clickable-looking action');
assert.equal(subscriptionAction.getAttribute('aria-disabled'), 'true');
assert.match(subscriptionState.textContent, /not open yet/i);
unavailableController.dispose();

console.log('dedicated cosmetics Shop exposes full pack details and deterministic item previews');
