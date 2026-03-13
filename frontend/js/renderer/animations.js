'use strict';

/**
 * Bot animations — idle bob, movement tilt, attack swing, death/respawn.
 * Uses per-frame updates (not Babylon Animation class) for simplicity.
 * @module renderer/animations
 */

const IDLE_BOB_SPEED = 2.5;
const IDLE_BOB_AMOUNT = 1.2;
const MOVE_BOB_SPEED = 6;
const MOVE_BOB_AMOUNT = 2.0;
const MOVE_TILT = 0.15;

/**
 * Exponential smoothing helper.
 * @param {number} current
 * @param {number} target
 * @param {number} rate - higher = faster convergence
 * @param {number} dt - frame delta in seconds
 * @returns {number}
 */
function lerp(current, target, rate, dt) {
  return current + (target - current) * (1 - Math.exp(-rate * dt));
}

/**
 * Per-frame animation state for a single bot.
 */
export class BotAnimState {
  constructor() {
    this.time = Math.random() * 10; // stagger bots
    this.prevX = 0;
    this.prevZ = 0;
    this.isMoving = false;
    this.moveAngle = 0;
    this.deathTimer = -1;  // -1 = not dying
    this.respawnTimer = -1;
    this.attackTimer = -1;
    // Smoothed values
    this.smoothY = 10;
    this.smoothRotX = 0;
    this.smoothRotZ = 0;
    this.targetRotY = null; // set externally when facing target
  }
}

/**
 * Update animation state and apply transforms.
 * @param {BotAnimState} anim
 * @param {BABYLON.Mesh} body - root body mesh
 * @param {BABYLON.Mesh|null} weapon - weapon mesh
 * @param {number} x - current x
 * @param {number} z - current z
 * @param {boolean} isAlive
 * @param {number} dt - frame delta in seconds
 */
export function updateBotAnim(anim, body, weapon, x, z, isAlive, dt) {
  anim.time += dt;

  // Detect movement
  const dx = x - anim.prevX;
  const dz = z - anim.prevZ;
  const speed = Math.sqrt(dx * dx + dz * dz);
  anim.isMoving = speed > 0.5;
  if (anim.isMoving) {
    anim.moveAngle = Math.atan2(dx, dz);
  }
  anim.prevX = x;
  anim.prevZ = z;

  // Death animation
  if (!isAlive) {
    if (anim.deathTimer < 0) anim.deathTimer = 0;
    anim.deathTimer = Math.min(anim.deathTimer + dt, 0.6);
    const t = anim.deathTimer / 0.6;
    body.rotation.z = t * (Math.PI / 2);
    body.scaling.y = Math.max(0.1, 1 - t * 0.8);
    if (body.material) body.material.alpha = 1 - t;
    return;
  }

  // Respawn recovery
  if (anim.deathTimer >= 0) {
    anim.deathTimer = -1;
    anim.respawnTimer = 0;
    body.rotation.z = 0;
    body.scaling.y = 1;
    if (body.material) body.material.alpha = 1;
  }
  if (anim.respawnTimer >= 0) {
    anim.respawnTimer += dt;
    const rt = Math.min(anim.respawnTimer / 0.5, 1);
    const glow = (1 - rt) * 0.8;
    if (body.material && body.material.emissiveColor) {
      body.material.emissiveColor.r = Math.min(body.material.emissiveColor.r + glow, 1);
      body.material.emissiveColor.g = Math.min(body.material.emissiveColor.g + glow, 1);
      body.material.emissiveColor.b = Math.min(body.material.emissiveColor.b + glow, 1);
    }
    if (anim.respawnTimer > 0.5) anim.respawnTimer = -1;
  }

  // Smooth rotation toward target when set
  if (anim.targetRotY !== null) {
    body.rotation.y = lerp(body.rotation.y, anim.targetRotY, 8, dt);
    anim.targetRotY = null;
  }

  // Attack swing — lunge + weapon swing (0.5s duration for visibility)
  if (anim.attackTimer >= 0) {
    anim.attackTimer += dt;
    const at = Math.min(anim.attackTimer / 0.5, 1);
    const swing = Math.sin(at * Math.PI);
    // Weapon swing arc (wider 2.2 radians)
    if (weapon) {
      weapon.rotation.z = -0.4 + swing * 2.2;
    }
    // Body lunge forward + tilt (smoothed)
    const targetRotX = swing * 0.25;
    const targetY = 10 + swing * 2;
    anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
    anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
    body.rotation.x = anim.smoothRotX;
    body.position.y = anim.smoothY;
    if (anim.attackTimer > 0.5) {
      anim.attackTimer = -1;
      // Let smoothing handle return — no hard snap
    }
    return;
  }

  // Idle / movement bob (smoothed)
  let targetY, targetRotX, targetRotZ;
  if (anim.isMoving) {
    const bob = Math.sin(anim.time * MOVE_BOB_SPEED) * MOVE_BOB_AMOUNT;
    targetY = 10 + bob;
    targetRotZ = Math.sin(anim.moveAngle) * MOVE_TILT;
    targetRotX = Math.cos(anim.moveAngle) * MOVE_TILT;
  } else {
    const bob = Math.sin(anim.time * IDLE_BOB_SPEED) * IDLE_BOB_AMOUNT;
    targetY = 10 + bob;
    targetRotZ = 0;
    targetRotX = 0;
  }
  anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
  anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
  anim.smoothRotZ = lerp(anim.smoothRotZ, targetRotZ, 8, dt);
  body.position.y = anim.smoothY;
  body.rotation.x = anim.smoothRotX;
  body.rotation.z = anim.smoothRotZ;
}

/**
 * Trigger an attack animation.
 * @param {BotAnimState} anim
 */
export function triggerAttack(anim) {
  anim.attackTimer = 0;
}
