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
      items: [
        {id: 'skin-standard', name: 'Standard', slot: 'bot_skin', is_free: true, rarity: 'common'},
        {id: 'skin-neon', name: 'Neon', slot: 'bot_skin', is_free: false, price_cents: 499, currency: 'USD', rarity: 'rare'},
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
assert.equal(findNode(catalogRoot, node => node.textContent === 'Email-account license'), null, 'disabled checkout must not advertise a purchasable license');
assert.match(status.textContent, /verified email account/i);

const html = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
assert.doesNotMatch(html, /data-cosmetic-key|Load my cosmetics|Clear key/);
assert.match(html, /href="dashboard\/"/);
const app = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
assert.doesNotMatch(app, /setKey\(data\.api_key/, 'key generation must not hand a bearer key to the public catalog preview');
assert.match(app, /initKeyGenerator\(keygenEl\)/);

console.log('public cosmetics catalog points ownership and assignment to the Bot Dashboard');
