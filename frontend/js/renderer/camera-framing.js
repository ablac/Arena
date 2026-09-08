'use strict';

const MIN_SHOT_SECONDS = 3;
const SWITCH_IMPROVEMENT = 0.65;

function livingPosition(bot) {
  return bot?.is_alive && Array.isArray(bot.position) &&
    Number.isFinite(bot.position[0]) && Number.isFinite(bot.position[1]);
}

function idOf(bot) { return String(bot.bot_id ?? bot.id ?? ''); }

function pairFrame(a, b, selectedAt) {
  const distance = Math.hypot(a.position[0] - b.position[0], a.position[1] - b.position[1]);
  const attack = [a, b].some(bot => ['attack', 'special'].includes(bot.action || bot.last_action));
  const ids = [idOf(a), idOf(b)].sort();
  return {
    ids, selectedAt,
    x: (a.position[0] + b.position[0]) / 2,
    z: (a.position[1] + b.position[1]) / 2,
    span: distance,
    score: distance * (attack ? 0.8 : 1),
  };
}

/** Select a stable nearby contest rather than the centroid of the entire map.
 * Called only for state snapshots, not every rendered frame. */
export function chooseCombatFrame(bots, previous = null, now = 0) {
  const alive = (Array.isArray(bots) ? bots : []).filter(livingPosition);
  alive.sort((a, b) => idOf(a).localeCompare(idOf(b)));
  if (!alive.length) return null;
  if (alive.length === 1) {
    const bot = alive[0];
    return { ids: [idOf(bot)], x: bot.position[0], z: bot.position[1], span: 0, score: 0,
      selectedAt: previous?.ids?.[0] === idOf(bot) ? previous.selectedAt : now };
  }

  let best = null;
  let current = null;
  for (let i = 0; i < alive.length; i++) {
    for (let j = i + 1; j < alive.length; j++) {
      const a = alive[i], b = alive[j];
      // FFA uses team0; only a positive shared team rules a pair out.
      if (Number(a.team) > 0 && a.team === b.team) continue;
      const candidate = pairFrame(a, b, now);
      if (!best || candidate.score < best.score) best = candidate;
      if (previous?.ids?.length === 2 && candidate.ids[0] === previous.ids[0] &&
          candidate.ids[1] === previous.ids[1]) current = candidate;
    }
  }
  // A surviving team can still be watched after its opponents are gone.
  if (!best) return { ids: [idOf(alive[0])], x: alive[0].position[0], z: alive[0].position[1],
    span: 0, score: 0, selectedAt: now };
  if (current && (now - previous.selectedAt < MIN_SHOT_SECONDS ||
      best.score >= current.score * SWITCH_IMPROVEMENT)) {
    current.selectedAt = previous.selectedAt;
    return current;
  }
  return best;
}

/** Keep both actors within the usable viewport, with breathing room for VFX. */
export function combatFrameRadius(span, viewport, fov = 0.8) {
  const distance = Number.isFinite(span) ? Math.max(0, span) : 0;
  const usableWidth = viewport?.width || 800;
  const usableHeight = viewport?.height || 600;
  const canvasHeight = viewport?.canvasHeight || usableHeight;
  const halfAngle = Math.max(0.2, Math.min(0.7, (Number.isFinite(fov) ? fov : 0.8) / 2));
  const visibleFraction = Math.max(0.25, Math.min(1, Math.min(usableWidth, usableHeight) / canvasHeight));
  const radius = (distance / 2 + 65) / (Math.tan(halfAngle) * visibleFraction);
  return Math.max(290, Math.min(1250, radius));
}
