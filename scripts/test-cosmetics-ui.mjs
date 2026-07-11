import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

class FakeElement {
  constructor(tagName = 'div') {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.dataset = {};
    this.className = '';
    this.textContent = '';
  }
  append(...nodes) { this.children.push(...nodes); }
  appendChild(node) { this.children.push(node); return node; }
  replaceChildren(...nodes) { this.children = [...nodes]; }
  querySelector() { return null; }
}

class FakeContainer extends FakeElement {
  constructor(elements) {
    super('section');
    this.elements = elements;
  }
  querySelector(selector) { return this.elements[selector] || null; }
}

function findNode(root, predicate) {
  if (predicate(root)) return root;
  for (const child of root.children || []) {
    const match = findNode(child, predicate);
    if (match) return match;
  }
  return null;
}

globalThis.document = {createElement: tagName => new FakeElement(tagName)};

let panelSource = readFileSync(new URL('../frontend/js/cosmetics-panel.js', import.meta.url), 'utf8');
panelSource = panelSource.replace(/import \{ apiPath \} from '[^']+';\r?\n/, "const apiPath = path => '/api/v1' + path;\n");
const {initCosmeticsPanel} = await import(dataModule(panelSource));

const status = new FakeElement('p');
const catalogRoot = new FakeElement('div');
const checkoutState = new FakeElement('span');
const panel = new FakeContainer({
  '[data-cosmetic-status]': status,
  '[data-cosmetic-catalog]': catalogRoot,
  '[data-cosmetic-checkout]': checkoutState,
});

let requestOptions;
globalThis.fetch = async (url, options = {}) => {
  assert.equal(url, '/api/v1/cosmetics/catalog');
  requestOptions = options;
  return {
    ok: true,
    json: async () => ({
      checkout_enabled: false,
      categories: [
        {id: 'chassis', name: 'Chassis', is_active: true, sort_order: 10},
        {id: 'starter-packs', name: 'Starter Packs', is_active: true, sort_order: 40},
      ],
      items: [
        {id: 'skin-standard', name: 'Standard', category_id: 'chassis', slot: 'bot_skin', is_free: true, rarity: 'common'},
        {id: 'skin-neon', name: 'Neon', category_id: 'chassis', slot: 'bot_skin', is_free: false, price_cents: 499, currency: 'USD', rarity: 'rare'},
      ],
      packs: [
        {
          id: 'neon-signal-pack',
          name: 'Neon Signal Pack',
          description: 'A coordinated three-piece Arena loadout.',
          category_id: 'starter-packs',
          price_cents: 99,
          currency: 'USD',
          is_purchasable: true,
          items: [
            {id: 'skin-neon', name: 'Neon'},
            {id: 'weapon-solar', name: 'Solar Flare'},
            {id: 'attachment-signal', name: 'Signal Antenna'},
          ],
        },
      ],
    }),
  };
};

const api = initCosmeticsPanel(panel);
await new Promise(resolve => setTimeout(resolve, 0));
assert.ok(api, 'panel should initialise without an API-key input');
assert.equal(requestOptions.cache, 'no-store');
assert.ok(findNode(catalogRoot, node => node.textContent === 'Starter item'));
assert.ok(findNode(catalogRoot, node => node.textContent === 'Coming soon'));
assert.ok(findNode(catalogRoot, node => node.textContent === 'Neon Signal Pack'), 'pack name should be rendered');
assert.ok(findNode(catalogRoot, node => node.textContent === 'Preview · $0.99'), 'disabled checkout should show the planned pack price');
assert.ok(findNode(catalogRoot, node => node.textContent === 'Solar Flare'), 'pack contents should be inspectable');
assert.equal(findNode(catalogRoot, node => node.textContent === 'Email-account license'), null, 'disabled checkout must not advertise a purchasable license');
assert.match(status.textContent, /verified email account/i);

const html = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
assert.doesNotMatch(html, /data-cosmetic-key|Load my cosmetics|Clear key/);
assert.match(html, /href="dashboard\/"/);
const app = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
assert.doesNotMatch(app, /setKey\(data\.api_key/, 'key generation must not hand a bearer key to the public catalog preview');
assert.match(app, /initKeyGenerator\(keygenEl\)/);

console.log('public cosmetics catalog points ownership and assignment to the Bot Dashboard');
