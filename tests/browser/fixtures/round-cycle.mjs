const bot = (id, name, position, options = {}) => ({
  bot_id: id,
  name,
  position,
  is_alive: options.isAlive ?? true,
  hp: options.hp ?? 100,
  max_hp: 100,
  weapon: options.weapon || 'sword',
  avatar_color: options.color || '#46d7ff',
  rotation: 0,
  last_action: 'wait',
  action_tick: 0,
  cooldown_remaining: 0,
  round_kills: options.kills ?? 0,
  kill_streak: options.streak ?? 0,
  shield_absorb: 0,
  mine_count: 0,
  grapple_charges: 0,
  is_bounty_target: options.bounty ?? false,
});

const safeZone = {
  center: [1000, 1000],
  radius: 900,
  target_center: [1000, 1000],
  target_radius: 900,
};

export function arenaState(roundNumber, options = {}) {
  const winnerPosition = options.winnerPosition || [920, 1040];
  return {
    type: 'arena_state',
    tick: roundNumber * 100 + (options.tickOffset || 0),
    round_number: roundNumber,
    round_time_remaining: 45,
    arena_size: [2000, 2000],
    map_shape: 'square',
    game_mode: 'ffa',
    bots: [
      bot('winner', 'Aurora', winnerPosition, {
        color: '#ffd34e', bounty: true, kills: 4, streak: 4,
      }),
      bot('rival', 'Cinder', [1130, 910], {
        color: '#ff5f73', weapon: 'bow', kills: 1, streak: 1,
      }),
      bot('scout', 'Vector', [1020, 1155], {
        color: '#7dff9f', weapon: 'spear', kills: 1,
      }),
    ],
    waiting_bots: [],
    obstacles: [],
    mask_rects: [],
    pickups: [],
    events: [],
    kill_feed: [],
    safe_zone: { ...safeZone },
    bounty_target: options.bountyTarget === undefined ? 'winner' : options.bountyTarget,
    sudden_death: false,
  };
}

export function roundEnd(roundNumber) {
  return {
    type: 'round_end',
    round_number: roundNumber,
    intermission_secs: 2,
    winner: { id: 'winner', name: 'Aurora', color: '#ffd34e' },
    next_map: {
      shape: 'square',
      arena_size: [2000, 2000],
      obstacles: [],
      mask_rects: [],
      safe_zone: { ...safeZone },
    },
  };
}

export function lobbyState(roundNumber) {
  return {
    type: 'lobby_state',
    round_number: roundNumber,
    countdown: 1,
    connected_bots: 3,
    min_bots: 2,
    waiting_bots: [],
  };
}
