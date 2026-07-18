import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

class FakeWebSocket {
  static OPEN = 1;
  static last = null;

  constructor(url) {
    this.url = url;
    this.readyState = FakeWebSocket.OPEN;
    this.closeCalls = 0;
    FakeWebSocket.last = this;
  }

  close() { this.closeCalls += 1; }
  send() {}
}

globalThis.WebSocket = FakeWebSocket;
const timers = [];
globalThis.setTimeout = (callback, delay) => {
  const timer = { callback, delay, cleared: false };
  timers.push(timer);
  return timer;
};
globalThis.clearTimeout = (timer) => { if (timer) timer.cleared = true; };
globalThis.setInterval = () => ({ interval: true });
globalThis.clearInterval = () => {};
const source = readFileSync(new URL('../frontend/js/spectator-ws.js', import.meta.url), 'utf8');
const { SpectatorSocket } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

const states = [];
const statuses = [];
const controls = [];
const socket = new SpectatorSocket('wss://arena.example/ws/spectator',
  (state) => states.push(state),
  (status) => statuses.push(status),
  (control) => controls.push(control));
socket.connect();
FakeWebSocket.last.onopen();

// Browser timers can resume before queued WebSocket message callbacks after a
// suspended/backgrounded tab. Transport health belongs to the server's
// ping/pong loop; a resumed JavaScript watchdog must not close this socket.
for (const timer of timers.filter((entry) => !entry.cleared && entry.delay >= 30000)) {
  timer.callback();
}
assert.equal(FakeWebSocket.last.closeCalls, 0,
  'client-side message-silence timers must not close a server-heartbeated socket');

FakeWebSocket.last.onmessage({ data: JSON.stringify({ type: 'heartbeat', paused: true }) });
assert.equal(states.length, 0, 'heartbeat should not be passed to renderer state');

FakeWebSocket.last.onmessage({ data: JSON.stringify({ type: 'service_status', revision: 3 }) });
assert.equal(states.length, 0, 'service status must not be passed to renderer state');
assert.equal(controls.length, 1, 'service status should use the control callback');

FakeWebSocket.last.onmessage({ data: JSON.stringify({ type: 'arena_state', bots: [] }) });
assert.equal(states.length, 1);
assert.equal(states[0].type, 'arena_state');

socket.disconnect();
console.log('spectator heartbeat client checks passed');
