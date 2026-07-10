import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

class FakeElement {
  constructor() {
    this.hidden = false;
    this.textContent = '';
    this.dataset = {};
    this.listeners = new Map();
  }
  querySelector(selector) {
    for (const candidate of selector.split(',').map((value) => value.trim())) {
      if (this.children?.[candidate]) return this.children[candidate];
    }
    return null;
  }
  addEventListener(type, fn) { this.listeners.set(type, fn); }
  removeEventListener(type) { this.listeners.delete(type); }
  removeAttribute(name) {
    if (name === 'data-kind') delete this.dataset.kind;
    if (name === 'data-severity') delete this.dataset.severity;
  }
  click() { this.listeners.get('click')?.(); }
}

const message = new FakeElement();
const dismiss = new FakeElement();
const root = new FakeElement();
root.children = {
  '[data-service-status-message]': message,
  '[data-service-status-dismiss]': dismiss,
};

const storage = new Map();
globalThis.window = { location: { pathname: '/', protocol: 'https:', host: 'arena.example' } };
globalThis.document = { getElementById: (id) => id === 'service-status-banner' ? root : null };
globalThis.sessionStorage = {
  getItem: (key) => storage.get(key) || null,
  setItem: (key, value) => storage.set(key, value),
};
globalThis.fetch = async () => ({
  ok: true,
  json: async () => ({ type: 'service_status', revision: 0, broadcast: null, maintenance: null }),
});

let source = readFileSync(new URL('../frontend/js/service-status.js', import.meta.url), 'utf8');
source = source.replace("import { apiPath } from './paths.js?v=20260710a';", "const apiPath = (path) => '/api/v1' + path;");
const module = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

module.handleServiceStatus({
  type: 'service_status', revision: 2, broadcast: { id: 2, severity: 'info', message: '<b>plain text</b>' }, maintenance: null,
});
const controller = module.initServiceStatus({ root, pollIntervalMs: 60000 });
assert.equal(root.hidden, false);
assert.equal(message.textContent, '<b>plain text</b>', 'notice must be rendered as text, not HTML');
assert.equal(root.dataset.kind, 'broadcast');
assert.equal(dismiss.hidden, false);

dismiss.click();
assert.equal(root.hidden, true, 'manual broadcast should be session-dismissible');

controller.handleStatus({
  type: 'service_status', revision: 3, broadcast: null,
  maintenance: { id: 3, severity: 'warning', message: 'Restarting now', retry_after_seconds: 60 },
});
assert.equal(root.hidden, false);
assert.equal(root.dataset.kind, 'maintenance');
assert.equal(dismiss.hidden, true, 'maintenance must not be dismissible');

assert.equal(controller.handleStatus({ type: 'service_status', revision: 2, broadcast: null, maintenance: null }), false, 'older revisions must be ignored');
assert.equal(message.textContent, 'Restarting now');
controller.handleStatus({ type: 'service_status', revision: 4, broadcast: null, maintenance: null });
assert.equal(root.hidden, true, 'clear snapshot should hide the banner');

controller.destroy();
console.log('service status UI checks passed');
