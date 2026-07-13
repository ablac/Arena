import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const html = readFileSync(new URL('../frontend/admin/index.html', import.meta.url), 'utf8');

assert.match(
  html,
  /function renderConnectionDiagnostics\(d\)/,
  'admin connection logs must share a diagnostic renderer',
);
for (const field of [
  'disconnect_source',
  'close_code',
  'close_reason',
  'duration_ms',
  'actions_received',
  'reconnect_preserved',
  'session_id',
  'transport_error',
]) {
  assert(
    html.includes(`d.${field}`),
    `admin connection diagnostics must render ${field}`,
  );
}
assert.equal(
  html.split('renderConnectionDiagnostics(d)').length - 1,
  3,
  'both live and historical connection rows must render diagnostics',
);

console.log('admin connection diagnostic rendering checks passed');
