import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const html = readFileSync(new URL('../frontend/admin/index.html', import.meta.url), 'utf8');
const count = (source, value) => source.split(value).length - 1;

assert.match(html, /class="tab-item" data-tab="broadcasts"[^>]*>Site Broadcast<\/div>/, 'Site Broadcast must have its own admin navigation item');

const panelStart = html.indexOf('<div class="panel" id="panel-broadcasts">');
const panelEnd = html.indexOf('<!-- Controls Panel -->', panelStart);
assert(panelStart >= 0 && panelEnd > panelStart, 'Site Broadcast must have a dedicated panel before Game Config');
const broadcastPanel = html.slice(panelStart, panelEnd);
const controlsPanel = html.slice(panelEnd, html.indexOf('</main>', panelEnd));

for (const id of [
  'broadcastMessage',
  'broadcastSeverity',
  'broadcastExpiryMinutes',
  'broadcastPublishBtn',
  'broadcastClearBtn',
  'broadcastResult',
  'broadcastCurrent',
  'broadcastHistory',
]) {
  assert.equal(count(html, `id="${id}"`), 1, `#${id} must remain unique`);
  assert(broadcastPanel.includes(`id="${id}"`), `#${id} must live in the dedicated Site Broadcast panel`);
  assert(!controlsPanel.includes(`id="${id}"`), `#${id} must not remain in Game Config`);
}

assert.match(broadcastPanel, /class="[^"]*broadcast-history[^"]*"/, 'broadcast history must have a dedicated log surface');
assert.doesNotMatch(broadcastPanel, /max-height:\s*120px/, 'broadcast history must not keep the cramped Game Config height');
assert.match(html, /broadcasts:\s*\['Tune \+ Publish',\s*'Site Broadcast'/, 'Site Broadcast must have panel metadata');
assert.match(html, /currentTab === 'broadcasts'\)\s*\{\s*loadBroadcasts\(\);\s*\}/, 'opening the Site Broadcast tab must load current state and history');
assert.match(html, /service_status_changed' && currentTab === 'broadcasts'/, 'broadcast SSE refreshes must target the Site Broadcast tab');

const loadOpsStart = html.indexOf('async function loadOpsConsole()');
const loadBroadcastsStart = html.indexOf('async function loadBroadcasts()', loadOpsStart);
assert(loadOpsStart >= 0 && loadBroadcastsStart > loadOpsStart, 'broadcast loader must follow the operations loader');
assert.doesNotMatch(html.slice(loadOpsStart, loadBroadcastsStart), /loadBroadcasts\(\)/, 'Game Config loading must not fetch Site Broadcast history');
assert.doesNotMatch(html, /filter\(\(event\) => event\.slot === 'broadcast'\)\.slice\(0,\s*8\)/, 'the dedicated history surface must render the full API event window');

console.log('admin Site Broadcast panel checks passed');
