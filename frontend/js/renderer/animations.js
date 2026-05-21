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
  sword:   { duration: 0.42, windupPct: 0.18, activePct: 0.48, restZ: -0.4, windupZ: -1.35, swingZ: 2.25, lunge: 0.28, bob: 2.4, weaponX: 0.75, weaponY: 0.65, pitch: 0.18, yaw: 0.10, glow: 0.0 },
  bow:     { duration: 0.78, windupPct: 0.34, activePct: 0.12, restZ: 0.02, windupZ: 0.42, swingZ: -0.62, lunge: -0.08, bob: 0.7, weaponX: -0.25, weaponY: 0.85, pitch: -0.12, yaw: -0.08, glow: 0.0 },
  daggers: { duration: 0.24, windupPct: 0.14, activePct: 0.56, restZ: 0.05, windupZ: -0.48, swingZ: 1.75, lunge: 0.34, bob: 1.8, weaponX: 0.55, weaponY: 0.45, pitch: 0.12, yaw: 0.16, glow: 0.0 },
  spear:   { duration: 0.56, windupPct: 0.24, activePct: 0.42, restZ: -0.26, windupZ: -0.92, swingZ: 0.62, lunge: 0.38, bob: 3.1, weaponX: 1.6, weaponY: 0.32, pitch: -0.08, yaw: 0.12, glow: 0.0 },
  staff:   { duration: 0.96, windupPct: 0.42, activePct: 0.26, restZ: 0.05, windupZ: 0.72, swingZ: -0.36, lunge: 0.16, bob: 4.2, weaponX: 0.28, weaponY: 1.1, pitch: -0.12, yaw: 0.18, glow: 1.0 },
  shield:  { duration: 0.64, windupPct: 0.24, activePct: 0.34, restZ: 0.08, windupZ: 0.52, swingZ: -1.02, lunge: 0.32, bob: 2.2, weaponX: -0.95, weaponY: 0.28, pitch: 0.08, yaw: -0.18, glow: 0.25 },
  grapple: { duration: 0.48, windupPct: 0.18, activePct: 0.36, restZ: -0.10, windupZ: -0.58, swingZ: 0.94, lunge: 0.26, bob: 1.8, weaponX: 0.9, weaponY: 0.28, pitch: -0.04, yaw: 0.2, glow: 0.2 },
  shove:   { duration: 0.35, windupPct: 0.15, activePct: 0.50, restZ: 0, windupZ: -0.2, swingZ: 0.3, lunge: 0.4, bob: 3, weaponX: 0.45, weaponY: 0.15, pitch: 0.0, yaw: 0.0, glow: 0.0 },
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

function updateBowDraw(weapon, drawT = 0) {
  if (!weapon || !weapon._bowString || !weapon._bowStringBasePath) return;
  const B = window.BABYLON;
  const base = weapon._bowStringBasePath;
  const pull = Math.max(0, Math.min(1, drawT)) * 3.2;
  const path = [
    base[0].clone(),
    new B.Vector3(base[1].x - pull, base[1].y, base[1].z),
    base[2].clone(),
  ];
  B.MeshBuilder.CreateTube(null, {
    path,
    radius: 0.15,
    tessellation: 4,
    cap: B.Mesh.CAP_ALL,
    instance: weapon._bowString,
  });
  if (weapon._bowLimb) {
    const s = 1 + Math.max(0, Math.min(1, drawT)) * 0.03;
    weapon._bowLimb.scaling.set(s, 1, s);
  }
}

function applyWeaponPose(weapon, cfg, swingT = 0, glowT = 0) {
  if (!weapon) return;
  weapon.position.x = (cfg.weaponX || 0) * swingT;
  weapon.position.y = (cfg.weaponY || 0) * swingT;
  weapon.rotation.x = (cfg.pitch || 0) * swingT;
  weapon.rotation.y = (cfg.yaw || 0) * swingT;
  weapon.scaling.set(1, 1, 1);

  if (weapon._children?.length) {
    for (const child of weapon._children) {
      const mat = child?.material;
      if (!mat || !mat.emissiveColor) continue;
      if (child.name?.includes('staff-orb')) {
        child.scaling.set(1 + glowT * 0.16, 1 + glowT * 0.16, 1 + glowT * 0.16);
        mat.emissiveColor.set(0.4 + glowT * 0.45, 0.15 + glowT * 0.18, 0.9 + glowT * 0.05);
      } else if (child.name?.includes('staff-halo')) {
        child.scaling.set(1 + glowT * 0.14, 1 + glowT * 0.14, 1 + glowT * 0.14);
        child.rotation.z += glowT * 0.035;
        mat.alpha = 1;
        mat.emissiveColor.set(0.78 + glowT * 0.15, 0.5 + glowT * 0.18, 1.0);
      } else if (child.name?.includes('shield-rim')) {
        child.scaling.set(1 + glowT * 0.05, 1 + glowT * 0.05, 1 + glowT * 0.05);
      } else if (child.name?.includes('shield-boss')) {
        child.scaling.set(1 + glowT * 0.08, 1 + glowT * 0.08, 1 + glowT * 0.08);
      }
    }
  }
}

function resetWeaponPose(weapon) {
  if (!weapon) return;
  weapon.position.x = 0;
  weapon.position.y = 0;
  weapon.rotation.x = 0;
  weapon.rotation.y = 0;
  weapon.scaling.set(1, 1, 1);

  if (weapon._children?.length) {
    for (const child of weapon._children) {
      if (!child?.scaling) continue;
      child.scaling.set(1, 1, 1);
    }
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
    const duration = anim.attackDuration > 0 ? anim.attackDuration : cfg.duration;
    const progress = Math.min(anim.attackTimer / duration, 1);

    const windupEnd = cfg.windupPct;
    const activeEnd = windupEnd + cfg.activePct;

    let weaponZ, targetRotX, targetY, bowDraw = 0, weaponPoseT = 0, glowT = 0;

    if (progress < windupEnd) {
      // Windup: anticipation — weapon pulls back, body braces
      const t = progress / windupEnd;
      const ease = t * t; // accelerating ease-in
      weaponZ = cfg.restZ + (cfg.windupZ - cfg.restZ) * ease;
      targetRotX = -cfg.lunge * 0.3 * ease;
      targetY = 10;
      weaponPoseT = ease * 0.75;
      glowT = ease * (cfg.glow || 0);
      if (anim.attackType === 'bow') bowDraw = ease;
    } else if (progress < activeEnd) {
      // Active: the strike — weapon sweeps through, body lunges
      const t = (progress - windupEnd) / cfg.activePct;
      const ease = Math.sin(t * Math.PI / 2); // decelerating ease-out
      weaponZ = cfg.windupZ + (cfg.swingZ - cfg.windupZ) * ease;
      targetRotX = cfg.lunge * ease;
      targetY = 10 + cfg.bob * ease;
      weaponPoseT = 0.75 + ease * 0.55;
      glowT = (0.45 + ease * 0.55) * (cfg.glow || 0);
      if (anim.attackType === 'bow') bowDraw = 1 - ease * 0.92;
    } else {
      // Recovery: follow-through — weapon returns, body settles
      const t = (progress - activeEnd) / (1 - activeEnd);
      const ease = t * t; // accelerating ease-in for smooth return
      weaponZ = cfg.swingZ + (cfg.restZ - cfg.swingZ) * ease;
      targetRotX = cfg.lunge * (1 - ease);
      targetY = 10 + cfg.bob * (1 - ease);
      weaponPoseT = Math.max(0, 1 - ease);
      glowT = Math.max(0, 1 - ease * 1.15) * (cfg.glow || 0);
      if (anim.attackType === 'bow') bowDraw = Math.max(0, 0.08 - ease * 0.08);
    }

    // Daggers special: rapid oscillating double-slash during active phase
    if (anim.attackType === 'daggers' && progress >= windupEnd && progress < activeEnd) {
      const t = (progress - windupEnd) / cfg.activePct;
      weaponZ = cfg.windupZ + Math.sin(t * Math.PI * 3) * 1.5;
    }

    if (weapon) {
      weapon.rotation.z = weaponZ;
      applyWeaponPose(weapon, cfg, weaponPoseT, glowT);
    }
    if (anim.attackType === 'bow') updateBowDraw(weapon, bowDraw);
    anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
    anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
    body.rotation.x = anim.smoothRotX;
    body.position.y = anim.smoothY;

    if (anim.attackTimer > duration) {
      anim.attackTimer = -1;
      anim.attackType = null;
      updateBowDraw(weapon, 0);
      resetWeaponPose(weapon);
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
  updateBowDraw(weapon, 0);
  resetWeaponPose(weapon);
}

/**
 * Trigger a weapon-specific attack animation.
 * Respects interruption rules: won't interrupt dodge or active attack phase.
 * @param {BotAnimState} anim
 * @param {string} [weapon='sword'] - weapon type name
 */
export function triggerAttack(anim, weapon, durationOverride) {
  if (anim.dodgeTimer >= 0) return; // dodge can't be interrupted by attack
  // If already attacking, only interrupt during recovery phase
  if (anim.attackTimer >= 0) {
    const curCfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const activeDuration = anim.attackDuration > 0 ? anim.attackDuration : curCfg.duration;
    const progress = anim.attackTimer / activeDuration;
    if (progress < curCfg.windupPct + curCfg.activePct) return; // in windup or active
  }
  const cfg = WEAPON_ANIMS[weapon] || WEAPON_ANIMS.sword;
  anim.attackTimer = 0;
  anim.attackType = weapon || 'sword';
  anim.attackDuration = Math.max(0.12, Number(durationOverride) || cfg.duration);
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
