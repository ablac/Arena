import assert from 'node:assert/strict';
import ArenaBot from '../sdk/nodejs/src/ArenaBot.js';

class StatusBot extends ArenaBot {
  constructor() {
    super('arena_test', 'ws://127.0.0.1:1/ws/bot');
    this.statuses = [];
  }

  async onTick() { return this.idle(); }
  async onServiceStatus(status) { this.statuses.push(status); }
}

const bot = new StatusBot();
const before = Date.now();
await bot._handleServiceStatus({
  type: 'service_status',
  revision: 4,
  maintenance: { message: 'Updating', retry_after_seconds: 60 },
});
assert.equal(bot.serviceStatus.revision, 4);
assert.equal(bot.statuses.length, 1);
assert.ok(bot._maintenanceRetryUntil >= before + 59_000);

await bot._handleServiceStatus({
  type: 'service_status',
  revision: 4,
  server_time: 'later',
  maintenance: { message: 'Restarting', phase: 'restarting', retry_after_seconds: 30 },
});
assert.equal(bot.statuses.length, 2, 'same-revision semantic changes must be delivered');

await bot._handleServiceStatus({
  type: 'service_status',
  revision: 4,
  server_time: 'even-later',
  maintenance: { message: 'Restarting', phase: 'restarting', retry_after_seconds: 30 },
});
assert.equal(bot.statuses.length, 2, 'server_time-only changes must be deduplicated');

await bot._handleServiceStatus({ type: 'service_status', revision: 3, maintenance: null });
assert.equal(bot.serviceStatus.revision, 4, 'stale status must not replace a newer revision');
assert.equal(bot.statuses.length, 2);

await bot._handleServiceStatus({ type: 'service_status', revision: 4, maintenance: null });
assert.equal(bot._maintenanceRetryUntil, 0, 'same-revision expiry clears must reset reconnect delay');
assert.equal(bot.statuses.length, 3);

await bot._handleServiceStatus({ type: 'service_status', revision: 5, maintenance: null });
assert.equal(bot.serviceStatus.revision, 5);
assert.equal(bot._maintenanceRetryUntil, 0);
assert.equal(bot.statuses.length, 4);

console.log('Node SDK service-status callback and reconnect delay pass');
