import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

class FakeWebSocket {
  static OPEN = 1;
  static last = null;

  constructor(url) {
    this.url = url;
    this.readyState = FakeWebSocket.OPEN;
    FakeWebSocket.last = this;
  }

  close() {}
  send() {}
}

globalThis.WebSocket = FakeWebSocket;
const source = readFileSync(new URL('../frontend/js/spectator-ws.js', import.meta.url), 'utf8');
const { SpectatorSocket } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

const states = [];
const statuses = [];
const socket = new SpectatorSocket('wss://arena.example/ws/spectator',
  (state) => states.push(state),
  (status) => statuses.push(status));
let staleResets = 0;
socket._resetStaleTimer = () => { staleResets += 1; };
socket.connect();

FakeWebSocket.last.onmessage({ data: JSON.stringify({ type: 'heartbeat', paused: true }) });
assert.equal(staleResets, 1, 'heartbeat should refresh stale timer');
assert.equal(states.length, 0, 'heartbeat should not be passed to renderer state');

FakeWebSocket.last.onmessage({ data: JSON.stringify({ type: 'arena_state', bots: [] }) });
assert.equal(staleResets, 2);
assert.equal(states.length, 1);
assert.equal(states[0].type, 'arena_state');
assert.ok(socket._staleTimeout > 30000, 'stale timeout should tolerate heartbeat jitter');

socket.disconnect();
console.log('spectator heartbeat client checks passed');
