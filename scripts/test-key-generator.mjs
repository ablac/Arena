import assert from 'node:assert/strict';
import {existsSync, readFileSync} from 'node:fs';

const mainHTML = readFileSync(new URL('../frontend/index.html', import.meta.url), 'utf8');
const appSource = readFileSync(new URL('../frontend/js/app.js', import.meta.url), 'utf8');
const generatorURL = new URL('../frontend/js/key-generator.js', import.meta.url);
const dashboardHTML = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const accountSource = readFileSync(new URL('../frontend/dashboard/account-cosmetics.js', import.meta.url), 'utf8');

assert.match(mainHTML, /id="keygen-card"/,
  'Get Started must contain the public token generator');
assert.match(mainHTML, /Generate API (?:key|token)/i);
assert.match(mainHTML, /No (?:signup|account) required/i,
  'public issuance must not be presented as requiring Dashboard login');
assert.match(mainHTML, /link (?:it|the bot|your bot).*Dashboard|Dashboard.*link/i,
  'Get Started must explain the later verified-email claim');
assert.match(appSource, /import \{ initKeyGenerator \} from '.\/key-generator\.js[^']*'/);
assert.match(appSource, /initKeyGenerator\(keygenEl\)/);
assert.equal(existsSync(generatorURL), true, 'the public key generator module must exist');

let source = readFileSync(generatorURL, 'utf8');
assert.match(source, /apiPath\('\/keys\/generate'\)/,
  'the browser must call the server issuance endpoint, never fabricate a credential');
assert.doesNotMatch(source, /localStorage|sessionStorage|indexedDB/i,
  'one-time plaintext credentials must not be persisted in browser storage');
source = source.replace(
  /import \{ apiPath \} from '[^']+';\r?\n/,
  "const apiPath = path => '/api/v1' + path;\n",
);

class FakeElement {
  constructor() {
    this.listeners = new Map();
    this.disabled = false;
    this.textContent = '';
    this.value = '';
    this.children = [];
    this.html = '';
  }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  async click() { await this.listeners.get('click')?.({currentTarget: this}); }
  set innerHTML(value) {
    this.html = value;
    this.keyField = String(value).includes('id="key-display"') ? new FakeElement() : null;
    if (this.keyField) this.keyField.value = 'arena_server_secret';
    this.copyButton = String(value).includes('data-keygen-copy') ? new FakeElement() : null;
    this.clearButton = String(value).includes('data-keygen-clear') ? new FakeElement() : null;
    this.warning = String(value).includes('keygen-warning') ? new FakeElement() : null;
  }
  get innerHTML() { return this.html; }
  querySelector(selector) {
    if (selector === '#key-display') return this.keyField || null;
    if (selector === '[data-keygen-copy]') return this.copyButton || null;
    if (selector === '[data-keygen-clear]') return this.clearButton || null;
    if (selector === '.keygen-warning') return this.warning || null;
    return null;
  }
  replaceChildren(...children) {
    this.children = children;
    this.html = '';
    this.keyField = null;
  }
}

const generateButton = new FakeElement();
const result = new FakeElement();
const container = {
  querySelector(selector) {
    if (selector === '.keygen-btn') return generateButton;
    if (selector === '.keygen-result') return result;
    return null;
  },
};
const copied = [];
Object.defineProperty(globalThis, 'navigator', {
  configurable: true,
  value: {clipboard: {writeText: async value => copied.push(value)}},
});
globalThis.document = {createElement: () => new FakeElement()};
let failNextGeneration = false;
globalThis.fetch = async (path, options) => {
  assert.equal(path, '/api/v1/keys/generate');
  assert.deepEqual(options, {method: 'POST'});
  if (failNextGeneration) {
    failNextGeneration = false;
    return {ok: false, status: 503, json: async () => ({detail: 'registration temporarily unavailable'})};
  }
  return {ok: true, json: async () => ({api_key: 'arena_server_secret', bot_id: 'bot-1'})};
};

const module = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
module.initKeyGenerator(container);
await generateButton.click();
assert.equal(result.keyField?.value, 'arena_server_secret');
const plaintextField = result.keyField;
await result.copyButton.click();
assert.deepEqual(copied, ['arena_server_secret']);
failNextGeneration = true;
await generateButton.click();
assert.equal(result.keyField, plaintextField,
  'a failed replacement request must not discard the previous valid one-time token');
assert.equal(plaintextField.value, 'arena_server_secret');
assert.match(result.warning?.textContent || '', /Previous token kept/i);
await result.clearButton.click();
assert.equal(plaintextField.value, '', 'Clear must zero the plaintext before removing its field');
assert.equal(result.keyField, null, 'Clear must zero and remove the plaintext token');

assert.match(accountSource, /bots:\s*'\/account\/bots'/);
assert.match(accountSource, /id="linkBotKey"/);
assert.match(dashboardHTML, /accountRoute\('bots'\)[\s\S]*api_key/,
  'verified Dashboard must claim a previously issued token and bot');
assert.match(dashboardHTML, /\binput\.value\s*=\s*''/,
  'Dashboard must clear the raw proof token after the claim request');

console.log('Get Started issues a server token that can later be claimed by a verified Dashboard account');
