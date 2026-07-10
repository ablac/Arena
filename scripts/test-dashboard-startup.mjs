import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const html = readFileSync(new URL('../frontend/dashboard/index.html', import.meta.url), 'utf8');
const inlineScripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)];
assert.equal(inlineScripts.length, 2, 'dashboard should retain its two classic inline scripts');
for (const script of inlineScripts) new Function(script[1]);

function extractFunction(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `missing ${name}`);
  const brace = source.indexOf('{', start);
  let depth = 0;
  for (let index = brace; index < source.length; index++) {
    if (source[index] === '{') depth++;
    if (source[index] === '}') {
      depth--;
      if (depth === 0) return source.slice(start, index + 1);
    }
  }
  throw new Error(`unterminated ${name}`);
}

const dashboardAPIBase = new Function(`return (${extractFunction(html, 'dashboardAPIBase')});`)();
assert.equal(dashboardAPIBase('/', null), '/api/v1');
assert.equal(dashboardAPIBase('/dashboard/', null), '/api/v1');
assert.equal(dashboardAPIBase('/arena', null), '/arena/api/v1');
assert.equal(dashboardAPIBase('/arena/dashboard/', null), '/arena/api/v1');
assert.equal(
  dashboardAPIBase('/arena/dashboard/', { apiBase: pathname => `helper:${pathname}` }),
  'helper:/arena/dashboard/',
);

const startup = extractFunction(html, 'startDashboard');
const importAt = startup.indexOf("await import('../js/paths.js?v=20260710a')");
const initAt = startup.lastIndexOf('initDashboardMode()');
assert.ok(importAt !== -1 && initAt > importAt, 'dashboard must await the path helper before public startup');
assert.doesNotMatch(
  html,
  /<script type="module">[^<]*ArenaPaths/,
  'the old deferred head bootstrap must not race the classic dashboard script',
);

console.log('dashboard startup awaits paths and retains a synchronous mount-aware fallback');
