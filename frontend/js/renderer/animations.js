'use strict';

/**
 * Bot animations — idle bob, movement tilt, per-weapon attack, dodge, death/respawn.
 * Uses per-frame updates (not Babylon Animation class) for simplicity.
 * @module renderer/animations
 */

const IDLE_BOB_SPEED = 2.5;
const IDLE_BOB_AMOUNT = 1.2;
const MOVE_BOB_SPEED = 6;
const MOVE_BOB_AMOUNT = 2.0;
const MOVE_TILT = 0.15;

/**
 * Per-weapon animation configs. Durations slightly undershoot server cooldowns
 * so animations finish before the next attack is possible.
 *
 * Phase percentages split the animation into windup → active → recovery.
 * restZ/windupZ/swingZ control weapon mesh rotation.z through each phase.
 * lunge/bob control body tilt and vertical offset during the strike.
 */
const WEAPON_ANIMS = {
  sword:   { duration: 0.45, windupPct: 0.20, activePct: 0.50, restZ: -0.4, windupZ: -1.2, swingZ: 2.0,  lunge: 0.25, bob: 2 },
  bow:     { duration: 0.85, windupPct: 0.30, activePct: 0.15, restZ: 0,    windupZ: 0.3,  swingZ: -0.5, lunge: -0.1, bob: 1 },
  daggers: { duration: 0.25, windupPct: 0.15, activePct: 0.55, restZ: 0,    windupZ: -0.3, swingZ: 1.5,  lunge: 0.3,  bob: 1.5 },
  spear:   { duration: 0.60, windupPct: 0.25, activePct: 0.40, restZ: -0.3, windupZ: -0.8, swingZ: 0.5,  lunge: 0.35, bob: 3 },
  staff:   { duration: 1.00, windupPct: 0.40, activePct: 0.30, restZ: 0,    windupZ: 0.5,  swingZ: -0.3, lunge: 0.15, bob: 4 },
  shield:  { duration: 0.70, windupPct: 0.25, activePct: 0.35, restZ: 0,    windupZ: 0.4,  swingZ: -0.8, lunge: 0.3,  bob: 2 },
  shove:   { duration: 0.35, windupPct: 0.15, activePct: 0.50, restZ: 0, windupZ: -0.2, swingZ: 0.3, lunge: 0.4, bob: 3 },
};

const DODGE_DURATION = 0.3;

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
    this.attackType = null;      // weapon name: 'sword', 'bow', etc.
    this.attackDuration = 0.5;   // set per weapon from WEAPON_ANIMS
    this.dodgeTimer = -1;
    this.dodgeAngle = 0;
    // Smoothed values
    this.smoothY = 10;
    this.smoothRotX = 0;
    this.smoothRotZ = 0;
    this.targetRotY = null; // set externally when facing target
  }
}

/**
 * Update animation state and apply transforms.
 * Priority order: death > respawn > dodge > attack > idle/movement.
 * @param {BotAnimState} anim
 * @param {BABYLON.TransformNode} body - root transform node
 * @param {BABYLON.Mesh|null} weapon - weapon mesh
 * @param {number} x - current x
 * @param {number} z - current z
 * @param {boolean} isAlive
 * @param {number} dt - frame delta in seconds
 * @param {BABYLON.StandardMaterial|null} bodyMat - body mesh material for alpha/emissive
 */
export function updateBotAnim(anim, body, weapon, x, z, isAlive, dt, bodyMat) {
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

  // --- Priority 1: Death animation (NEVER interrupted) ---
  if (!isAlive) {
    if (anim.deathTimer < 0) anim.deathTimer = 0;
    anim.deathTimer = Math.min(anim.deathTimer + dt, 0.6);
    const t = anim.deathTimer / 0.6;
    body.rotation.z = t * (Math.PI / 2);
    body.scaling.y = Math.max(0.1, 1 - t * 0.8);
    if (bodyMat) bodyMat.alpha = 1 - t;
    return;
  }

  // --- Priority 2: Respawn recovery (NEVER interrupted) ---
  if (anim.deathTimer >= 0) {
    anim.deathTimer = -1;
    anim.respawnTimer = 0;
    anim.dodgeTimer = -1;
    anim.attackTimer = -1;
    anim.attackType = null;
    body.rotation.z = 0;
    body.scaling.y = 1;
    if (bodyMat) bodyMat.alpha = 1;
  }
  if (anim.respawnTimer >= 0) {
    anim.respawnTimer += dt;
    const rt = Math.min(anim.respawnTimer / 0.5, 1);
    const glow = (1 - rt) * 0.8;
    if (bodyMat && bodyMat.emissiveColor) {
      bodyMat.emissiveColor.r = Math.min(bodyMat.emissiveColor.r + glow, 1);
      bodyMat.emissiveColor.g = Math.min(bodyMat.emissiveColor.g + glow, 1);
      bodyMat.emissiveColor.b = Math.min(bodyMat.emissiveColor.b + glow, 1);
    }
    if (anim.respawnTimer > 0.5) anim.respawnTimer = -1;
  }

  // Smooth rotation toward target when set
  if (anim.targetRotY !== null) {
    body.rotation.y = lerp(body.rotation.y, anim.targetRotY, 8, dt);
    // Clear once close enough to prevent jitter
    const diff = Math.abs(body.rotation.y - anim.targetRotY);
    if (diff < 0.05) {
      anim.targetRotY = null;
    }
  }

  // --- Priority 3: Dodge dash (only interrupted by death) ---
  if (anim.dodgeTimer >= 0) {
    anim.dodgeTimer += dt;
    const t = Math.min(anim.dodgeTimer / DODGE_DURATION, 1);
    const wave = Math.sin(t * Math.PI);
    // Squish body + rapid transparency flicker for invuln shimmer
    body.scaling.y = 1 - wave * 0.3;
    body.scaling.x = 1 + wave * 0.2;
    body.scaling.z = 1 + wave * 0.2;
    if (bodyMat) {
      bodyMat.alpha = 0.5 + Math.sin(t * Math.PI * 4) * 0.3;
    }
    if (anim.dodgeTimer > DODGE_DURATION) {
      anim.dodgeTimer = -1;
      body.scaling.set(1, 1, 1);
      if (bodyMat) bodyMat.alpha = 1;
    }
    return;
  }

  // --- Priority 4–6: Weapon-specific attack ---
  if (anim.attackTimer >= 0) {
    anim.attackTimer += dt;
    const cfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const progress = Math.min(anim.attackTimer / cfg.duration, 1);

    const windupEnd = cfg.windupPct;
    const activeEnd = windupEnd + cfg.activePct;

    let weaponZ, targetRotX, targetY;

    if (progress < windupEnd) {
      // Windup: anticipation — weapon pulls back, body braces
      const t = progress / windupEnd;
      const ease = t * t; // accelerating ease-in
      weaponZ = cfg.restZ + (cfg.windupZ - cfg.restZ) * ease;
      targetRotX = -cfg.lunge * 0.3 * ease;
      targetY = 10;
    } else if (progress < activeEnd) {
      // Active: the strike — weapon sweeps through, body lunges
      const t = (progress - windupEnd) / cfg.activePct;
      const ease = Math.sin(t * Math.PI / 2); // decelerating ease-out
      weaponZ = cfg.windupZ + (cfg.swingZ - cfg.windupZ) * ease;
      targetRotX = cfg.lunge * ease;
      targetY = 10 + cfg.bob * ease;
    } else {
      // Recovery: follow-through — weapon returns, body settles
      const t = (progress - activeEnd) / (1 - activeEnd);
      const ease = t * t; // accelerating ease-in for smooth return
      weaponZ = cfg.swingZ + (cfg.restZ - cfg.swingZ) * ease;
      targetRotX = cfg.lunge * (1 - ease);
      targetY = 10 + cfg.bob * (1 - ease);
    }

    // Daggers special: rapid oscillating double-slash during active phase
    if (anim.attackType === 'daggers' && progress >= windupEnd && progress < activeEnd) {
      const t = (progress - windupEnd) / cfg.activePct;
      weaponZ = cfg.windupZ + Math.sin(t * Math.PI * 3) * 1.5;
    }

    if (weapon) weapon.rotation.z = weaponZ;
    anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
    anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
    body.rotation.x = anim.smoothRotX;
    body.position.y = anim.smoothY;

    if (anim.attackTimer > cfg.duration) {
      anim.attackTimer = -1;
      anim.attackType = null;
      // Let smoothing handle return — no hard snap
    }
    return;
  }

  // --- Idle / movement bob (smoothed) ---
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
 * Trigger a weapon-specific attack animation.
 * Respects interruption rules: won't interrupt dodge or active attack phase.
 * @param {BotAnimState} anim
 * @param {string} [weapon='sword'] - weapon type name
 */
export function triggerAttack(anim, weapon) {
  if (anim.dodgeTimer >= 0) return; // dodge can't be interrupted by attack
  // If already attacking, only interrupt during recovery phase
  if (anim.attackTimer >= 0) {
    const curCfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const progress = anim.attackTimer / curCfg.duration;
    if (progress < curCfg.windupPct + curCfg.activePct) return; // in windup or active
  }
  const cfg = WEAPON_ANIMS[weapon] || WEAPON_ANIMS.sword;
  anim.attackTimer = 0;
  anim.attackType = weapon || 'sword';
  anim.attackDuration = cfg.duration;
}

/**
 * Trigger a dodge dash animation (0.3s shimmer + squish).
 * Can interrupt windup/recovery attacks but not active strike phase.
 * @param {BotAnimState} anim
 * @param {number} [angle=0] - dodge direction angle
 */
/**
 * Trigger a shove animation (shoulder-check push).
 * Uses the attack animation system with shove-specific config.
 * @param {BotAnimState} anim
 */
export function triggerShove(anim) {
  if (anim.dodgeTimer >= 0) return;
  if (anim.attackTimer >= 0) {
    const curCfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const progress = anim.attackTimer / curCfg.duration;
    if (progress < curCfg.windupPct + curCfg.activePct) return;
  }
  anim.attackTimer = 0;
  anim.attackType = 'shove';
  anim.attackDuration = WEAPON_ANIMS.shove.duration;
}

export function triggerDodge(anim, angle) {
  if (anim.deathTimer >= 0) return;
  if (anim.dodgeTimer >= 0) return; // already dodging
  anim.dodgeTimer = 0;
  anim.dodgeAngle = angle || 0;
  anim.attackTimer = -1;
  anim.attackType = null;
}
