import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const ArenaSSEFrameParser = require('../frontend/admin/sse-parser.js');
const events = [];
const parser = new ArenaSSEFrameParser(event => events.push(event));

// Deliberately split field names, JSON, CRLF pairs, and frame boundaries.
for (const chunk of [
  ': connected\n\nid: 41\neve',
  'nt: game_event\nda',
  'ta: {"round":',
  '7}\n\nid: 42\r\nevent: error\r\ndata: first\r',
  '\ndata: second\r\n\r\n',
]) {
  parser.push(chunk);
}
parser.end();

assert.deepEqual(events, [
  {type: 'game_event', data: '{"round":7}', id: '41'},
  {type: 'error', data: 'first\nsecond', id: '42'},
]);

console.log('frontend/admin/sse-parser.js: split-chunk parsing passes');
