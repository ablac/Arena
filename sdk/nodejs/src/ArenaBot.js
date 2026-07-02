import WebSocket from 'ws';
import { distance, directionToward, directionAway } from './helpers.js';

/**
 * Base class for AI Battle Arena bots.
 * Subclass and override {@link onTick} (required), plus optionally
 * {@link onMapInit}, {@link onDeath}, {@link onRespawn}, and {@link onRoundEnd}.
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

    // Terrain cache (populated by map_init)
    /** @type {string[][]|null} */ this._terrain = null;
    /** @type {number} */ this._mapWidth = 0;
    /** @type {number} */ this._mapHeight = 0;
    /** @type {number} */ this._cellSize = 1;
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
  /**
   * Called once at the start of each round with map data.
   * Default implementation stores the terrain grid.
   * @param {string[][]} terrain  row-major terrain grid
   * @param {number} width   map width in tiles
   * @param {number} height  map height in tiles
   */
  async onMapInit(terrain, width, height) {}
  /** @param {object} deathInfo */
  async onDeath(deathInfo) {}
  /** @param {object} respawnInfo */
  async onRespawn(respawnInfo) {}
  /** @param {object} roundInfo */
  async onRoundEnd(roundInfo) {}

  // ── Action helpers ─────────────────────────────────────────────

  /** Move toward a target position (returns grid direction with -1/0/1 components). */
  moveToward(myPos, targetPos) {
    return { action: 'move', direction: directionToward(myPos, targetPos) };
  }
  /** Move away from a threat position (returns grid direction with -1/0/1 components). */
  moveAway(myPos, threatPos) {
    return { action: 'move', direction: directionAway(myPos, threatPos) };
  }
  /** Move to an exact grid position [col, row]. */
  moveTo(targetPos) {
    return { action: 'move_to', target_position: [targetPos[0], targetPos[1]] };
  }
  /** Attack a target by ID. For staff, pass targetPosition [col, row] for area attack. */
  attack(targetId, targetPosition) {
    const a = { action: 'attack', target: targetId };
    if (targetPosition) a.direction = [targetPosition[0], targetPosition[1]];
    return a;
  }
  /** Staff area attack at a position [col, row]. */
  staffAttack(targetPosition) {
    return { action: 'attack', direction: [targetPosition[0], targetPosition[1]] };
  }
  /** Shove a target — knocks them back far with a short stun, no damage. */
  shove(targetId) { return { action: 'shove', target: targetId }; }
  /** Dodge in a direction. */
  dodge(direction) { return { action: 'dodge', direction }; }
  /** Use an item by ID. */
  useItem(itemId) { return { action: 'use_item', item_id: itemId }; }
  /** Place a landmine at current position (max 3 per bot, arms after delay). */
  placeMine() { return { action: 'place_mine' }; }
  /** Deploy a gravity well at a target position [col, row]. Requires a charge from pickup. */
  useGravityWell(targetPosition) {
    return { action: 'use_gravity_well', target_position: [targetPosition[0], targetPosition[1]] };
  }
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

  // ── Map / pathfinding helpers ──────────────────────────────────

  /**
   * Build an ASCII view of the local area around the bot.
   * Merges cached terrain with entity positions.
   * Legend: '@' = self, 'B' = bot, 'P' = pickup, '.' = ground, '#' = wall
   * @param {object} state      your_state from tick
   * @param {object[]} nearby   nearby_entities from tick
   * @param {number} [radius=5] view radius in tiles
   * @returns {string[]}        array of strings (one per row)
   */
  getLocalMap(state, nearby, radius = 5) {
    const [myCol, myRow] = state.position;
    const size = radius * 2 + 1;
    const rows = [];

    for (let dr = -radius; dr <= radius; dr++) {
      let row = '';
      for (let dc = -radius; dc <= radius; dc++) {
        const c = myCol + dc;
        const r = myRow + dr;
        if (dc === 0 && dr === 0) {
          row += '@';
        } else if (this._terrain && r >= 0 && r < this._mapHeight && c >= 0 && c < this._mapWidth) {
          row += this._terrain[r][c];
        } else {
          row += ' ';
        }
      }
      rows.push(row);
    }

    // Overlay entities
    for (const ent of (nearby || [])) {
      if (!ent.position) continue;
      const [ec, er] = ent.position;
      const dc = ec - myCol + radius;
      const dr = er - myRow + radius;
      if (dc < 0 || dc >= size || dr < 0 || dr >= size) continue;
      if (dc === radius && dr === radius) continue; // skip self position
      const ch = ent.type === 'bot' ? 'B' : ent.type === 'pickup' ? 'P' : '?';
      const line = rows[dr];
      rows[dr] = line.substring(0, dc) + ch + line.substring(dc + 1);
    }

    return rows;
  }

  /**
   * A* pathfinding on the cached terrain grid.
   * Walls ('#') and void ('V') are impassable.
   * @param {number[]} start  [col, row]
   * @param {number[]} goal   [col, row]
   * @returns {number[][]}    array of [col, row] waypoints (excluding start, including goal), or [] if no path
   */
  findPath(start, goal) {
    if (!this._terrain) return [];
    const [sc, sr] = start;
    const [gc, gr] = goal;
    if (sc === gc && sr === gr) return [];

    const w = this._mapWidth;
    const h = this._mapHeight;

    const isPassable = (c, r) => {
      if (c < 0 || c >= w || r < 0 || r >= h) return false;
      const cell = this._terrain[r][c];
      return cell !== '#' && cell !== 'V';
    };

    if (!isPassable(gc, gr)) return [];

    // Chebyshev heuristic
    const heuristic = (c, r) => Math.max(Math.abs(c - gc), Math.abs(r - gr));

    const key = (c, r) => r * w + c;
    const gScore = new Map();
    const cameFrom = new Map();
    gScore.set(key(sc, sr), 0);

    // Simple priority queue using sorted array
    const open = [{ c: sc, r: sr, f: heuristic(sc, sr) }];
    const closed = new Set();

    const DIRS = [
      [-1, -1], [0, -1], [1, -1],
      [-1,  0],          [1,  0],
      [-1,  1], [0,  1], [1,  1],
    ];

    while (open.length > 0) {
      // Find node with lowest f
      let bestIdx = 0;
      for (let i = 1; i < open.length; i++) {
        if (open[i].f < open[bestIdx].f) bestIdx = i;
      }
      const current = open[bestIdx];
      open.splice(bestIdx, 1);

      const ck = key(current.c, current.r);
      if (closed.has(ck)) continue;
      closed.add(ck);

      if (current.c === gc && current.r === gr) {
        // Reconstruct path
        const path = [];
        let k = ck;
        while (cameFrom.has(k)) {
          const r = Math.floor(k / w);
          const c = k % w;
          path.push([c, r]);
          k = cameFrom.get(k);
        }
        path.reverse();
        return path;
      }

      const currentG = gScore.get(ck);

      for (const [dc, dr] of DIRS) {
        const nc = current.c + dc;
        const nr = current.r + dr;
        if (!isPassable(nc, nr)) continue;
        const nk = key(nc, nr);
        if (closed.has(nk)) continue;

        const tentG = currentG + 1;
        const prevG = gScore.get(nk);
        if (prevG !== undefined && tentG >= prevG) continue;

        gScore.set(nk, tentG);
        cameFrom.set(nk, ck);
        open.push({ c: nc, r: nr, f: tentG + heuristic(nc, nr) });
      }
    }

    return []; // no path found
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
      case 'map_init':
        // Normalise compact row-string format to 2D char array
        if (msg.terrain && msg.terrain.length > 0 && typeof msg.terrain[0] === 'string') {
          this._terrain = msg.terrain.map(row => row.split(''));
        } else {
          this._terrain = msg.terrain;
        }
        this._mapWidth = msg.width;
        this._mapHeight = msg.height;
        this._cellSize = msg.cell_size || 1;
        console.log(`[ArenaBot] Map loaded: ${msg.width}x${msg.height} (cell_size=${this._cellSize})`);
        await this.onMapInit(msg.terrain, msg.width, msg.height);
        break;
      case 'tick': {
        const st = msg.your_state || {};
        if (st.position) this._lastPos = st.position;
        this.lastActionResult = st.last_action_result || null;
        // Team number in team-based game modes (0 = no team / FFA).
        this.team = st.team || 0;
        const safeZone = {
          center: st.zone_center || [0, 0],
          radius: st.zone_radius ?? 100,
          in_safe_zone: st.in_safe_zone ?? true,
          distance_to_edge: st.distance_to_zone_edge || 0,
        };
        if (msg.fog_radius !== undefined) {
          safeZone.fog_radius = msg.fog_radius;
        }
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
