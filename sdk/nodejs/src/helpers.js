/**
 * Helper functions for AI Battle Arena bots.
 * @module helpers
 */

/**
 * Extract x and y from a position (supports arrays and {x, y} objects).
 * @param {number[]|{x: number, y: number}} pos
 * @returns {[number, number]}
 */
export function posXY(pos) {
  if (Array.isArray(pos)) return [pos[0], pos[1]];
  return [pos.x, pos.y];
}

/**
 * Euclidean distance between two positions.
 * @param {number[]|{x: number, y: number}} a
 * @param {number[]|{x: number, y: number}} b
 * @returns {number}
 */
export function distance(a, b) {
  const [ax, ay] = posXY(a);
  const [bx, by] = posXY(b);
  const dx = bx - ax;
  const dy = by - ay;
  return Math.sqrt(dx * dx + dy * dy);
}

/**
 * Normalize a direction vector to unit length.
 * Returns [0, 0] for zero-length vectors.
 * @param {number} dx
 * @param {number} dy
 * @returns {[number, number]}
 */
export function normalize(dx, dy) {
  const len = Math.sqrt(dx * dx + dy * dy);
  if (len === 0) return [0, 0];
  return [dx / len, dy / len];
}

/**
 * Normalized direction vector from one position toward another.
 * @param {number[]|{x: number, y: number}} from
 * @param {number[]|{x: number, y: number}} to
 * @returns {[number, number]}
 */
export function directionToward(from, to) {
  const [fx, fy] = posXY(from);
  const [tx, ty] = posXY(to);
  return normalize(tx - fx, ty - fy);
}

/**
 * Normalized direction vector away from a position.
 * @param {number[]|{x: number, y: number}} from
 * @param {number[]|{x: number, y: number}} threat
 * @returns {[number, number]}
 */
export function directionAway(from, threat) {
  const [fx, fy] = posXY(from);
  const [tx, ty] = posXY(threat);
  return normalize(fx - tx, fy - ty);
}
