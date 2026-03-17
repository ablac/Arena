/**
 * Helper functions for AI Battle Arena bots.
 * @module helpers
 */

/**
 * Extract col and row from a position (supports arrays and {x, y} objects).
 * @param {number[]|{x: number, y: number}} pos
 * @returns {[number, number]}  [col, row]
 */
export function posXY(pos) {
  if (Array.isArray(pos)) return [pos[0], pos[1]];
  return [pos.x, pos.y];
}

/**
 * Chebyshev distance between two grid positions.
 * @param {number[]|{x: number, y: number}} a
 * @param {number[]|{x: number, y: number}} b
 * @returns {number}
 */
export function distance(a, b) {
  const [ac, ar] = posXY(a);
  const [bc, br] = posXY(b);
  return Math.max(Math.abs(bc - ac), Math.abs(br - ar));
}

/**
 * Grid direction from one position toward another.
 * Each component is -1, 0, or 1.
 * @param {number[]|{x: number, y: number}} from
 * @param {number[]|{x: number, y: number}} to
 * @returns {[number, number]}  [dcol, drow]
 */
export function directionToward(from, to) {
  const [fc, fr] = posXY(from);
  const [tc, tr] = posXY(to);
  return [Math.sign(tc - fc), Math.sign(tr - fr)];
}

/**
 * Grid direction away from a threat position.
 * Each component is -1, 0, or 1.
 * @param {number[]|{x: number, y: number}} from
 * @param {number[]|{x: number, y: number}} threat
 * @returns {[number, number]}  [dcol, drow]
 */
export function directionAway(from, threat) {
  const [fc, fr] = posXY(from);
  const [tc, tr] = posXY(threat);
  return [Math.sign(fc - tc), Math.sign(fr - tr)];
}
