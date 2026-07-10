import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

function dataModule(source) {
  return `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
}

class FakeButton {
  constructor() {
    this.listeners = [];
    this.disabled = false;
    this.textContent = '';
  }
  addEventListener(type, listener) {
    if (type === 'click') this.listeners.push(listener);
  }
  async click() {
    for (const listener of this.listeners) await listener({type:'click',target:this});
  }
}

class FakeResult {
  constructor() {
    this.keyField = null;
    this.clearButton = new FakeButton();
    this.removed = false;
  }
  set innerHTML(value) {
    if (String(value).includes('id="key-display"')) this.keyField = {value:'arena_secret'};
  }
  querySelector(selector) {
    if (selector === '#key-display') return this.keyField;
    if (selector === '[data-keygen-clear]') return this.clearButton;
    return null;
  }
  replaceChildren() {
    assert.equal(this.keyField?.value, '', 'the plaintext key must be zeroed before its DOM is removed');
    this.removed = true;
  }
}

const sourcePath = new URL('../frontend/js/key-generator.js', import.meta.url);
let source = readFileSync(sourcePath, 'utf8');
assert.match(source, /addEventListener\('click', \(\) => requestArenaAPIKeyClear\(\)\)/, 'click events must not be passed as the credential-event target');
source = source
  .replace(/import \{ apiPath \} from '[^']+';\r?\n/, "const apiPath = path => '/api/v1' + path;\n")
  .replace(
    /import \{ onArenaAPIKeyClear, requestArenaAPIKeyClear \} from '[^']+';\r?\n/,
    'let clearListener; const onArenaAPIKeyClear = listener => { clearListener = listener; }; const requestArenaAPIKeyClear = () => clearListener?.();\n',
  );

globalThis.document = {createElement: () => ({textContent:'',innerHTML:''})};
globalThis.fetch = async () => ({ok:true,json:async () => ({api_key:'arena_secret',bot_id:'bot-1'})});
const {initKeyGenerator} = await import(dataModule(source));
const generateButton = new FakeButton();
const result = new FakeResult();
const container = {
  querySelector(selector) {
    if (selector === '.keygen-btn') return generateButton;
    if (selector === '.keygen-result') return result;
    return null;
  },
};

initKeyGenerator(container);
await generateButton.click();
assert.equal(result.keyField?.value, 'arena_secret');
await result.clearButton.click();
assert.equal(result.removed, true, 'clicking Clear key should dispatch the page-wide clear event');

console.log('generated API key has a reachable zero-and-clear control');
