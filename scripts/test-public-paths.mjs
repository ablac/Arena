import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('../frontend/js/paths.js', import.meta.url), 'utf8');
const paths = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

assert.equal(paths.mountPrefix('/m/'), '');
assert.equal(paths.appPath('/shop/', '/'), '/shop/');
assert.equal(paths.apiPath('/leaderboard', '/m/'), '/api/v1/leaderboard');
assert.equal(paths.appPath('/m/', '/'), '/m/');
assert.equal(paths.wsURL('/spectator', {
  pathname: '/m/', protocol: 'https:', host: 'arena.example',
}), 'wss://arena.example/ws/spectator');

assert.equal(paths.mountPrefix('/arena/dashboard/'), '/arena');
assert.equal(paths.appPath('/shop/', '/arena/'), '/arena/shop/');
assert.equal(paths.apiPath('/leaderboard', '/arena/dashboard/'), '/arena/api/v1/leaderboard');
assert.equal(paths.appPath('/m/', '/arena/'), '/arena/m/');
assert.equal(paths.appPath('/dashboard/?view=public', '/arena/'), '/arena/dashboard/?view=public');
assert.equal(paths.wsURL('/spectator', {
  pathname: '/arena/m/', protocol: 'https:', host: 'arena.example',
}), 'wss://arena.example/arena/ws/spectator');

assert.equal(paths.apiPath('/version', '/arena'), '/arena/api/v1/version');
assert.equal(paths.wsURL('/spectator', {
  pathname: '/arena', protocol: 'http:', host: 'localhost:8000',
}), 'ws://localhost:8000/arena/ws/spectator');

console.log('public path helper checks passed');
