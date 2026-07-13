'use strict';

/**
 * Allocation-stable Forge character motion.
 *
 * The sampler owns one Float32Array per bot. Each class changes posture and
 * timing through its roster profile, while combat actions retain clear
 * anticipation/contact/recovery poses under reduced-motion settings.
 * @module renderer/character-anims
 */

export const POSE_CHANNELS = Object.freeze([
  'bodyY', 'bodyPitch', 'bodyRoll', 'headPitch', 'headYaw',
  'armLPitch', 'armLRoll', 'elbowLPitch',
  'armRPitch', 'armRRoll', 'elbowRPitch',
  'legLPitch', 'kneeLPitch', 'legRPitch', 'kneeRPitch',
  'weaponX', 'weaponY', 'weaponZ', 'weaponPitch', 'weaponYaw', 'weaponRoll',
  'corePulse',
]);

const P = Object.freeze(Object.fromEntries(POSE_CHANNELS.map((name, index) => [name, index])));
const TAU = Math.PI * 2;
const FORGE_ATTACK_TIMING = Object.freeze({
  sword: Object.freeze({duration: 0.48, contact: 0.42}),
  bow: Object.freeze({duration: 0.78, contact: 0.40}),
  spear: Object.freeze({duration: 0.58, contact: 0.45}),
  daggers: Object.freeze({duration: 0.30, contact: 0.42}),
  staff: Object.freeze({duration: 0.92, contact: 0.55}),
  shield: Object.freeze({duration: 0.64, contact: 0.41}),
  grapple: Object.freeze({duration: 0.52, contact: 0.36}),
});

function clamp01(value) {
  return Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
}

function smooth(value) {
  const t = clamp01(value);
  return t * t * (3 - 2 * t);
}

function actionEnvelope(t) {
  if (t < 0) return 0;
  if (t < 0.28) return smooth(t / 0.28);
  if (t < 0.62) return 1;
  return 1 - smooth((t - 0.62) / 0.38);
}

export class ForgeAnimState {
  constructor(weapon = 'sword') {
    this.weapon = weapon;
    this.pose = new Float32Array(POSE_CHANNELS.length);
    this.attackTimer = -1;
    this.attackDuration = 0.5;
    this.attackType = weapon;
    this.dodgeTimer = -1;
    this.dodgeDuration = 0.36;
    this.dodgeAngle = 0;
    this.shoveTimer = -1;
    this.shoveDuration = 0.34;
    this.hitTimer = -1;
    this.hitDuration = 0.18;
    this.hitStrength = 0;
    this.deathTimer = -1;
    this.deathDuration = 0.92;
    this.respawnTimer = -1;
    this.respawnDuration = 0.55;
    this.elapsed = 0;
    this.gaitPhase = 0;
    this.woundLevel = 0;
    this.targetRotY = 0;
    this.moveAngle = 0;
    this.wasAlive = true;
  }
}

function forgeAttackDuration(weapon, durationOverride) {
  const timing = FORGE_ATTACK_TIMING[weapon] || FORGE_ATTACK_TIMING.sword;
  const override = Number(durationOverride);
  return Number.isFinite(override) && override > 0.16
    ? Math.min(override, 1.4)
    : timing.duration;
}

/** Delay effects until the Forge pose reaches its visible contact frame. */
export function forgeContactDelay(weapon, durationOverride) {
  const timing = FORGE_ATTACK_TIMING[weapon] || FORGE_ATTACK_TIMING.sword;
  return timing.contact * forgeAttackDuration(weapon, durationOverride);
}

export function triggerForgeAttack(state, weapon = 'sword', durationOverride, replace = false) {
  if (!state || state.deathTimer >= 0 || (state.attackTimer >= 0 && !replace)) return false;
  state.attackType = weapon;
  state.attackDuration = forgeAttackDuration(weapon, durationOverride);
  state.attackTimer = 0;
  return true;
}

export function triggerForgeDodge(state, angle = 0) {
  if (!state || state.deathTimer >= 0 || state.dodgeTimer >= 0) return false;
  state.dodgeAngle = Number.isFinite(angle) ? angle : 0;
  state.dodgeTimer = 0;
  return true;
}

export function triggerForgeShove(state) {
  if (!state || state.deathTimer >= 0 || state.shoveTimer >= 0) return false;
  state.shoveTimer = 0;
  return true;
}

export function triggerForgeHit(state, strength = 0.5) {
  if (!state || state.deathTimer >= 0) return false;
  state.hitStrength = clamp01(strength);
  state.hitTimer = 0;
  return true;
}

function advance(state, timerKey, durationKey, dt, keepCompleted = false) {
  const timer = state[timerKey];
  if (timer < 0) return -1;
  const duration = Math.max(0.001, state[durationKey]);
  const next = timer + dt;
  if (next >= duration) {
    state[timerKey] = keepCompleted ? duration : -1;
    return keepCompleted ? 1 : -1;
  }
  state[timerKey] = next;
  return clamp01(next / duration);
}

function applyAttackPose(pose, weapon, t) {
  const amount = actionEnvelope(t);
  if (!amount) return;
  const strike = t < 0.28 ? -smooth(t / 0.28) : smooth((t - 0.28) / 0.34);
  switch (weapon) {
    case 'bow': {
      const draw = t < 0.62 ? amount : Math.max(0, 1 - (t - 0.62) / 0.08);
      pose[P.bodyRoll] -= 0.16 * draw;
      pose[P.armLPitch] -= 1.18 * draw;
      pose[P.armLRoll] -= 0.34 * draw;
      pose[P.armRPitch] -= 0.92 * draw;
      pose[P.armRRoll] += 0.96 * draw;
      pose[P.elbowRPitch] -= 1.26 * draw;
      pose[P.weaponZ] -= 0.16 * draw;
      pose[P.weaponYaw] += 0.22 * draw;
      break;
    }
    case 'spear':
      pose[P.bodyPitch] += 0.24 * amount;
      pose[P.armRPitch] -= 1.12 * amount;
      pose[P.elbowRPitch] -= 0.42 * amount;
      pose[P.armLPitch] -= 0.82 * amount;
      pose[P.weaponZ] += 4.8 * strike;
      pose[P.weaponPitch] += 0.22 * amount;
      pose[P.legLPitch] -= 0.42 * amount;
      pose[P.legRPitch] += 0.32 * amount;
      break;
    case 'daggers': {
      const side = Math.sin(Math.max(0, t) * Math.PI * 3);
      pose[P.bodyRoll] += 0.28 * side * amount;
      pose[P.armLPitch] -= (0.92 - 0.35 * side) * amount;
      pose[P.armRPitch] -= (0.92 + 0.35 * side) * amount;
      pose[P.armLRoll] -= 0.54 * amount;
      pose[P.armRRoll] += 0.54 * amount;
      pose[P.weaponRoll] += 1.2 * side * amount;
      break;
    }
    case 'staff': {
      const charge = t < 0.55 ? smooth(t / 0.55) : amount;
      pose[P.bodyY] += 0.34 * charge;
      pose[P.armLPitch] -= 0.82 * charge;
      pose[P.armRPitch] -= 1.02 * charge;
      pose[P.elbowLPitch] -= 0.72 * charge;
      pose[P.elbowRPitch] -= 0.58 * charge;
      pose[P.weaponPitch] -= 0.42 * charge;
      pose[P.corePulse] += 0.55 * charge;
      break;
    }
    case 'shield':
      pose[P.bodyPitch] += 0.30 * amount;
      pose[P.bodyRoll] -= 0.12 * amount;
      pose[P.armLPitch] -= 1.18 * amount;
      pose[P.elbowLPitch] -= 0.48 * amount;
      pose[P.weaponZ] += 2.8 * strike;
      pose[P.legLPitch] -= 0.32 * amount;
      pose[P.legRPitch] += 0.26 * amount;
      break;
    case 'grapple':
      pose[P.bodyPitch] += 0.20 * amount;
      pose[P.bodyRoll] += 0.18 * amount;
      pose[P.armRPitch] -= 1.30 * amount;
      pose[P.elbowRPitch] -= 0.20 * amount;
      pose[P.weaponZ] += 3.2 * strike;
      pose[P.weaponPitch] += 0.28 * amount;
      pose[P.corePulse] += 0.30 * amount;
      break;
    case 'sword':
    default:
      pose[P.bodyPitch] += 0.18 * amount;
      pose[P.bodyRoll] += 0.30 * strike * amount;
      pose[P.armRPitch] -= 1.04 * amount;
      pose[P.armRRoll] += 0.58 * strike * amount;
      pose[P.elbowRPitch] -= 0.32 * amount;
      pose[P.armLPitch] -= 0.40 * amount;
      pose[P.weaponRoll] += 1.85 * strike * amount;
      pose[P.weaponZ] += 1.25 * amount;
      break;
  }
}

/**
 * Advance state and return its reused pose buffer.
 * @param {Object} profile character-roster profile
 * @param {ForgeAnimState} state
 * @param {number} dt seconds
 * @param {boolean} movingNow
 * @param {number} speedNow normalized visual speed
 * @param {boolean} alive
 * @param {boolean} reducedMotion
 */
export function sampleForgePose(
  profile, state, dt,
  movingNow = false, speedNow = 0, alive = true, reducedMotion = false,
) {
  const step = Math.max(0, Math.min(Number.isFinite(dt) ? dt : 0, 0.1));
  const pose = state.pose;
  pose.fill(0);
  state.elapsed += step;

  if (!alive && state.wasAlive) {
    state.deathTimer = 0;
    state.attackTimer = -1;
    state.dodgeTimer = -1;
    state.shoveTimer = -1;
  } else if (alive && !state.wasAlive) {
    state.deathTimer = -1;
    state.respawnTimer = 0;
  }
  state.wasAlive = alive;

  const speed = clamp01(speedNow);
  const moving = movingNow === true && speed > 0.01;
  const restrained = reducedMotion === true;
  if (moving) state.gaitPhase += step * profile.motion.strideHz * TAU * (0.55 + speed * 0.65);

  if (!restrained) {
    pose[P.bodyY] = Math.sin(state.elapsed * (1.1 + profile.motion.weight * 0.25) * TAU)
      * profile.motion.bob * (moving ? 0.28 : 0.14);
    pose[P.bodyRoll] = Math.sin(state.elapsed * 1.7) * profile.motion.sway * 0.18;
    pose[P.headYaw] = Math.sin(state.elapsed * 0.63) * 0.045;
  }

  if (moving) {
    const gait = Math.sin(state.gaitPhase);
    const gaitScale = (restrained ? 0.36 : 1) * (0.42 + speed * 0.58);
    pose[P.legLPitch] += gait * 0.62 * gaitScale;
    pose[P.legRPitch] -= gait * 0.62 * gaitScale;
    pose[P.kneeLPitch] += Math.max(0, -gait) * 0.52 * gaitScale;
    pose[P.kneeRPitch] += Math.max(0, gait) * 0.52 * gaitScale;
    pose[P.armLPitch] -= gait * 0.34 * gaitScale;
    pose[P.armRPitch] += gait * 0.34 * gaitScale;
    if (!restrained) pose[P.bodyY] += Math.abs(Math.cos(state.gaitPhase)) * profile.motion.bob * 0.46 * gaitScale;
  }

  const attackT = advance(state, 'attackTimer', 'attackDuration', step);
  if (attackT >= 0) applyAttackPose(pose, state.attackType || profile.weapon, attackT);

  const shoveT = advance(state, 'shoveTimer', 'shoveDuration', step);
  if (shoveT >= 0) {
    const amount = actionEnvelope(shoveT);
    pose[P.bodyPitch] += 0.22 * amount;
    pose[P.armLPitch] -= 0.92 * amount;
    pose[P.armRPitch] -= 0.92 * amount;
    pose[P.weaponZ] += 1.2 * amount;
  }

  const dodgeT = advance(state, 'dodgeTimer', 'dodgeDuration', step);
  if (dodgeT >= 0) {
    const amount = Math.sin(dodgeT * Math.PI);
    pose[P.bodyY] -= 1.0 * amount;
    pose[P.bodyPitch] += 0.34 * amount;
    pose[P.bodyRoll] += Math.sin(state.dodgeAngle) * 0.42 * amount;
    pose[P.legLPitch] -= 0.48 * amount;
    pose[P.legRPitch] += 0.32 * amount;
  }

  const hitT = advance(state, 'hitTimer', 'hitDuration', step);
  if (hitT >= 0) {
    const amount = (1 - hitT) * state.hitStrength;
    pose[P.headPitch] -= 0.42 * amount;
    pose[P.bodyPitch] -= 0.24 * amount;
  }

  if (state.woundLevel > 0 && alive) {
    pose[P.bodyPitch] += state.woundLevel * 0.055;
    pose[P.headPitch] += state.woundLevel * 0.035;
  }

  const deathT = advance(state, 'deathTimer', 'deathDuration', step, true);
  if (deathT >= 0) {
    const fall = smooth(deathT);
    pose[P.bodyY] -= 7.5 * fall;
    pose[P.bodyPitch] += 0.72 * fall;
    pose[P.bodyRoll] += 1.20 * fall;
    pose[P.armLPitch] += 0.55 * fall;
    pose[P.armRPitch] -= 0.35 * fall;
  }

  const respawnT = advance(state, 'respawnTimer', 'respawnDuration', step);
  if (respawnT >= 0) pose[P.bodyY] -= (1 - smooth(respawnT)) * 4.5;

  pose[P.corePulse] += restrained ? 0 : Math.sin(state.elapsed * 2.2) * 0.06;
  return pose;
}

function shortestAngle(from, to) {
  let delta = (to - from) % TAU;
  if (delta > Math.PI) delta -= TAU;
  if (delta < -Math.PI) delta += TAU;
  return delta;
}

/** Apply the sampled numeric pose to one Forge rig. */
export function updateForgeCharacter(entry, dt, reducedMotion = false, highDetail = true) {
  if (!entry?.joints || !entry.anim) return;
  const root = entry.root;
  const lastX = Number.isFinite(entry._poseX) ? entry._poseX : root.position.x;
  const lastZ = Number.isFinite(entry._poseZ) ? entry._poseZ : root.position.z;
  const dx = root.position.x - lastX;
  const dz = root.position.z - lastZ;
  entry._poseX = root.position.x;
  entry._poseZ = root.position.z;
  const speed = Math.min(1, Math.hypot(dx, dz) / Math.max(0.001, dt * 14));
  const moving = dx * dx + dz * dz > 0.0004;
  if (moving && entry.anim.attackTimer < 0) {
    entry.anim.targetRotY = Math.atan2(dx, dz);
    entry.anim.moveAngle = entry.anim.targetRotY;
  }
  root.rotation.y += shortestAngle(root.rotation.y, entry.anim.targetRotY) * Math.min(1, dt * 10);

  const pose = sampleForgePose(
    entry.profile, entry.anim, dt,
    moving, speed, entry.isAlive, reducedMotion,
  );
  // Distant bots still advance action/death clocks and facing, but their
  // articulated meshes are disabled, so rewriting every joint is wasted work.
  if (!highDetail) return;
  const j = entry.joints;
  j.body.position.y = entry.basePose.bodyY + pose[P.bodyY];
  j.body.rotation.x = entry.profile.proportions.posture + pose[P.bodyPitch];
  j.body.rotation.z = pose[P.bodyRoll];
  j.head.rotation.x = pose[P.headPitch];
  j.head.rotation.y = pose[P.headYaw];
  j.leftArm.rotation.x = pose[P.armLPitch];
  j.leftArm.rotation.z = entry.basePose.armLRoll + pose[P.armLRoll];
  j.leftElbow.rotation.x = entry.basePose.elbowLPitch + pose[P.elbowLPitch];
  j.rightArm.rotation.x = pose[P.armRPitch];
  j.rightArm.rotation.z = entry.basePose.armRRoll + pose[P.armRRoll];
  j.rightElbow.rotation.x = entry.basePose.elbowRPitch + pose[P.elbowRPitch];
  j.leftLeg.rotation.x = pose[P.legLPitch];
  j.leftKnee.rotation.x = entry.basePose.kneePitch + pose[P.kneeLPitch];
  j.rightLeg.rotation.x = pose[P.legRPitch];
  j.rightKnee.rotation.x = entry.basePose.kneePitch + pose[P.kneeRPitch];

  const weaponNodes = entry.weaponPoseNodes || (entry.weapon ? [entry.weapon] : []);
  const weaponBases = entry.weaponBases || (entry.weaponBase ? [entry.weaponBase] : []);
  for (let index = 0; index < weaponNodes.length; index += 1) {
    const node = weaponNodes[index];
    const base = weaponBases[index];
    if (!node || !base) continue;
    node.position.x = base.x + pose[P.weaponX];
    node.position.y = base.y + pose[P.weaponY];
    node.position.z = base.z + pose[P.weaponZ];
    node.rotation.x = base.rx + pose[P.weaponPitch];
    node.rotation.y = base.ry + pose[P.weaponYaw];
    node.rotation.z = base.rz + pose[P.weaponRoll] * (base.sign || 1);
  }
  if (j.core) {
    const scale = Math.max(0.82, 1 + pose[P.corePulse]);
    j.core.scaling.setAll(scale);
  }
}
