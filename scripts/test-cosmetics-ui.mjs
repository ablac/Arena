import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return { promise, resolve };
}

class FakeElement {
  constructor(tagName = 'div') {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.listeners = new Map();
    this.dataset = {};
    this.className = '';
    this.textContent = '';
    this.value = '';
    this.disabled = false;
    this.id = '';
  }

  append(...nodes) { this.children.push(...nodes); }
  appendChild(node) { this.children.push(node); return node; }
  replaceChildren(...nodes) { this.children = [...nodes]; }
  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) || [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }
  dispatch(type, event = {}) {
    for (const listener of this.listeners.get(type) || []) listener({ type, target: this, ...event });
  }
  querySelector(selector) {
    if (selector.startsWith('#')) return findNode(this, node => node.id === selector.slice(1));
    return null;
  }
}

class FakeContainer extends FakeElement {
  constructor(elements) {
    super('section');
    this.elements = elements;
  }
  querySelector(selector) { return this.elements[selector] || super.querySelector(selector); }
}

function findNode(root, predicate) {
  if (predicate(root)) return root;
  for (const child of root.children || []) {
    const match = findNode(child, predicate);
    if (match) return match;
  }
  return null;
}

const credentialSource = readFileSync(new URL('../frontend/js/credential-events.js', import.meta.url), 'utf8');
const credentials = await import(dataModule(credentialSource));

const gate = credentials.createLatestRequestGate();
const publicVersion = gate.next();
const authenticatedVersion = gate.next();
assert.equal(gate.isCurrent(publicVersion), false, 'older public response must be stale');
assert.equal(gate.isCurrent(authenticatedVersion), true, 'new authenticated response must remain current');

globalThis.window = new EventTarget();
globalThis.document = { createElement: tagName => new FakeElement(tagName) };
globalThis.__arenaCredentialTest = credentials;

let panelSource = readFileSync(new URL('../frontend/js/cosmetics-panel.js', import.meta.url), 'utf8');
panelSource = panelSource
  .replace(/import \{ apiPath \} from '[^']+';\r?\n/, "const apiPath = path => '/api/v1' + path;\n")
  .replace(
    /import \{[\s\S]*?\} from '\.\/credential-events\.js\?v=20260710a';\r?\n/,
    'const { createLatestRequestGate, onArenaAPIKeyClear, requestArenaAPIKeyClear } = globalThis.__arenaCredentialTest;\n',
  );
const { initCosmeticsPanel } = await import(dataModule(panelSource));

const keyInput = new FakeElement('input');
const loadButton = new FakeElement('button');
const clearButton = new FakeElement('button');
const status = new FakeElement('p');
const catalogRoot = new FakeElement('div');
const checkoutState = new FakeElement('span');
const panel = new FakeContainer({
  '[data-cosmetic-key]': keyInput,
  '[data-cosmetic-load]': loadButton,
  '[data-cosmetic-clear]': clearButton,
  '[data-cosmetic-status]': status,
  '[data-cosmetic-catalog]': catalogRoot,
  '[data-cosmetic-checkout]': checkoutState,
});

const publicResponse = deferred();
globalThis.fetch = (url, options = {}) => {
  if (url.endsWith('/cosmetics/catalog')) return publicResponse.promise;
  if (url.endsWith('/bot/cosmetics') && !options.method) {
    return Promise.resolve({
      ok: true,
      json: async () => ({
        items: [{
          id: 'skin-owned', name: 'Owned Skin', description: '', slot: 'bot_skin',
          rarity: 'common', is_free: false, owned: true, equipped: false,
        }],
      }),
    });
  }
  throw new Error(`unexpected fetch: ${url}`);
};

initCosmeticsPanel(panel);
keyInput.value = 'arena_secret';
loadButton.dispatch('click');
await new Promise(resolve => setTimeout(resolve, 0));
assert.ok(findNode(catalogRoot, node => node.textContent === 'Equip'), 'authenticated inventory should render');

publicResponse.resolve({
  ok: true,
  json: async () => ({
    checkout_enabled: false,
    items: [{ id: 'skin-preview', name: 'Preview', slot: 'bot_skin', is_free: false }],
  }),
});
await new Promise(resolve => setTimeout(resolve, 0));
assert.ok(findNode(catalogRoot, node => node.textContent === 'Equip'), 'late public response must not replace inventory');
assert.equal(findNode(catalogRoot, node => node.textContent === 'Load bot key'), null);

let keygenSource = readFileSync(new URL('../frontend/js/key-generator.js', import.meta.url), 'utf8');
keygenSource = keygenSource
  .replace(/import \{ apiPath \} from '[^']+';\r?\n/, "const apiPath = path => '/api/v1' + path;\n")
  .replace(
    /import \{ onArenaAPIKeyClear \} from '[^']+';\r?\n/,
    'const { onArenaAPIKeyClear } = globalThis.__arenaCredentialTest;\n',
  );
const { initKeyGenerator } = await import(dataModule(keygenSource));

const keygenButton = new FakeElement('button');
const keygenResult = new FakeElement('div');
const generatedField = new FakeElement('input');
generatedField.id = 'key-display';
generatedField.value = 'arena_secret';
keygenResult.append(generatedField);
const keygen = new FakeContainer({
  '.keygen-btn': keygenButton,
  '.keygen-result': keygenResult,
});
initKeyGenerator(keygen);

clearButton.dispatch('click');
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(keyInput.value, '', 'cosmetics input should be zeroed');
assert.equal(generatedField.value, '', 'generated key field should be zeroed before removal');
assert.equal(keygenResult.children.length, 0, 'generated credential DOM should be removed');

console.log('cosmetics request ordering and page-wide API-key clearing pass');
