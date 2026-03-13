import WebSocket from 'ws';
import { distance, directionToward, directionAway } from './helpers.js';

/**
 * Base class for AI Battle Arena bots.
 * Subclass and override {@link onTick} (required), plus optionally
 * {@link onDeath}, {@link onRespawn}, and {@link onRoundEnd}.
 */
export default class ArenaBot {
  /** @param {string} apiKey  @param {string} [serverUrl] */
  constructor(apiKey, serverUrl = 'wss://angel-serv.com/ws/bot') {
    this.apiKey = apiKey;
    this.serverUrl = serverUrl;
    /** @type {WebSocket|null} */ this.ws = null;
    /** @type {string|null} */ this.botId = null;
    /** @type {object|null} */ this.serverInfo = null;
    this._weapon = 'sword';
    this._stats = { hp: 5, speed: 5, attack: 5, defense: 5 };
    this._fallback = 'aggressive';
    this._running = false;
    this._lastPos = [0, 0];
  }

  /**
   * Configure the loadout sent on connect.
   * @param {string} weapon  @param {object} stats  @param {string} [fallback]
   */
  setLoadout(weapon, stats, fallback = 'aggressive') {
    this._weapon = weapon;
    this._stats = stats;
    this._fallback = fallback;
  }

  // ── Override these ─────────────────────────────────────────────

  /** Called every tick. Must return an action object. */
  async onTick(state, nearby, safeZone) { throw new Error('Implement onTick()'); }
  /** @param {object} deathInfo */
  async onDeath(deathInfo) {}
  /** @param {object} respawnInfo */
  async onRespawn(respawnInfo) {}
  /** @param {object} roundInfo */
  async onRoundEnd(roundInfo) {}

  // ── Action helpers ─────────────────────────────────────────────

  /** Move toward a target position. */
  moveToward(myPos, targetPos) {
    return { action: 'move', direction: directionToward(myPos, targetPos) };
  }
  /** Move away from a threat position. */
  moveAway(myPos, threatPos) {
    return { action: 'move', direction: directionAway(myPos, threatPos) };
  }
  /** Attack a target by ID. For staff, pass targetPosition [x,y] for area attack. */
  attack(targetId, targetPosition) {
    const a = { action: 'attack', target: targetId };
    if (targetPosition) a.direction = [targetPosition[0], targetPosition[1]];
    return a;
  }
  /** Staff area attack at a position [x, y]. */
  staffAttack(targetPosition) {
    return { action: 'attack', direction: [targetPosition[0], targetPosition[1]] };
  }
  /** Shove a target — knocks them back far with a short stun, no damage. */
  shove(targetId) { return { action: 'shove', target: targetId }; }
  /** Dodge in a direction. */
  dodge(direction) { return { action: 'dodge', direction }; }
  /** Use an item by ID. */
  useItem(itemId) { return { action: 'use_item', item_id: itemId }; }
  /** Do nothing this tick. */
  idle() { return { action: 'idle' }; }

  // ── Query helpers ──────────────────────────────────────────────

  /** Find the closest enemy bot in the nearby list. */
  closestEnemy(nearby) {
    const bots = (nearby || []).filter((e) => e.type === 'bot' && (e.id || e.bot_id) !== this.botId);
    if (!bots.length) return null;
    const myPos = this._lastPos;
    return bots.reduce((best, b) =>
      distance(myPos, b.position) < distance(myPos, best.position) ? b : best);
  }

  /** Find the enemy bot with the lowest HP. */
  lowestHpEnemy(nearby) {
    const bots = (nearby || []).filter((e) => e.type === 'bot' && (e.id || e.bot_id) !== this.botId);
    if (!bots.length) return null;
    return bots.reduce((best, b) =>
      (b.hp ?? Infinity) < (best.hp ?? Infinity) ? b : best);
  }

  /** Return nearby pickups sorted by distance (closest first). */
  nearbyPickups(nearby) {
    const myPos = this._lastPos;
    return (nearby || [])
      .filter((e) => e.type === 'pickup')
      .sort((a, b) => distance(myPos, a.position) - distance(myPos, b.position));
  }

  // ── Connection ─────────────────────────────────────────────────

  /** Open the WebSocket, send loadout, and wait for confirmation. */
  async connect() {
    const url = `${this.serverUrl}?key=${this.apiKey}`;
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(url);
      this.ws.on('open', () => console.log('[ArenaBot] Connected'));
      this.ws.on('message', async (raw) => {
        let msg;
        try { msg = JSON.parse(raw); } catch { return; }
        try {
          await this._handleMessage(msg, resolve);
        } catch (err) {
          console.error('[ArenaBot] Handler error:', err.message);
        }
      });
      this.ws.on('error', (err) => {
        console.error('[ArenaBot] WebSocket error:', err.message);
        reject(err);
      });
      this.ws.on('close', (code) => {
        console.log(`[ArenaBot] Disconnected (code=${code})`);
      });
    });
  }

  /** @private */
  async _handleMessage(msg, onReady) {
    switch (msg.type) {
      case 'connected':
        this.botId = msg.bot_id;
        this.serverInfo = msg;
        console.log(`[ArenaBot] Bot ID: ${this.botId}`);
        this._send({
          type: 'select_loadout', weapon: this._weapon,
          stats: this._stats, fallback_behavior: this._fallback,
        });
        break;
      case 'loadout_confirmed':
        console.log(`[ArenaBot] Loadout confirmed: ${msg.weapon}`);
        if (onReady) onReady();
        break;
      case 'tick': {
        const st = msg.your_state || {};
        if (st.position) this._lastPos = st.position;
        this.lastActionResult = st.last_action_result || null;
        const safeZone = {
          center: st.zone_center || [0, 0],
          radius: st.zone_radius ?? 100,
          in_safe_zone: st.in_safe_zone ?? true,
          distance_to_edge: st.distance_to_zone_edge || 0,
        };
        const action = await this.onTick(st, msg.nearby_entities, safeZone);
        if (action) {
          this._send({ type: 'action', tick: msg.tick_number, ...action });
        }
        break;
      }
      case 'death':
        console.log(`[ArenaBot] Died — killed by ${msg.killed_by}`);
        await this.onDeath(msg);
        break;
      case 'respawn':
        console.log('[ArenaBot] Respawned');
        await this.onRespawn(msg);
        break;
      case 'round_end':
        console.log(`[ArenaBot] Round ${msg.round_number} ended`);
        await this.onRoundEnd(msg);
        break;
      case 'error':
        console.error(`[ArenaBot] Server error: ${msg.message}`);
        break;
      case 'kick':
        console.error(`[ArenaBot] Kicked: ${msg.reason}`);
        this.ws?.close();
        break;
    }
  }

  /** @private */
  _send(data) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  /** Run the bot with automatic reconnection (exponential backoff). */
  async run() {
    this._running = true;
    let delay = 1000;
    while (this._running) {
      try {
        await this.connect();
        delay = 1000;
        await new Promise((resolve) => { this.ws.on('close', resolve); });
      } catch { /* connection failed */ }
      if (!this._running) break;
      console.log(`[ArenaBot] Reconnecting in ${delay / 1000}s...`);
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, 30000);
    }
  }
}
