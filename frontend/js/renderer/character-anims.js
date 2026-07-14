'use strict';

/**
 * Allocation-stable Forge character motion.
 *
 * The sampler owns one Float32Array per bot. Each class changes posture and
 * timing through its roster profile, while combat actions retain clear
 * anticipation/contact/recovery poses under reduced-motion settings.
 *
 * Sign conventions: the Forge model is authored facing local -Z (the visor,
 * chest core, and toes all sit at negative Z; character-rig turns the model
 * once so gameplay yaw stays +Z-forward). Pose channels are therefore
 * authored in CHARACTER semantics, not raw rig space:
 *   - bodyPitch / headPitch: positive leans or nods FORWARD (toward the face)
 *   - armPitch: positive swings the arm FORWARD/up; elbowPitch: positive bends
 *   - legPitch: positive strides FORWARD; kneePitch: positive tucks the shin
 *   - weaponZ: positive thrusts the weapon FORWARD along the facing direction
 *   - bodyYaw / roll / weapon rotations: raw rig-space radians
 * updateForgeCharacter() is the single place semantics map onto rig-space
 * signs, and the mapping differs by joint: torso/head content sits ABOVE its
 * joint so forward = negative rig pitch, while arms/legs hang BELOW theirs so
 * forward = positive rig pitch, and a knee tuck is negative. The old renderer
 * applied one sign to everything, which is why high-posture classes leaned
 * backward, attack swings wound up behind the body, and thrust attacks fired
 * into the character's own back.
 * @module renderer/character-anims
 */

export const POSE_CHANNELS = Object.freeze([
  'bodyY', 'bodyPitch', 'bodyRoll', 'bodyYaw', 'headPitch', 'headYaw',
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
  bow: Object.freeze({duration: 0.78, contact: 0.58}),
  spear: Object.freeze({duration: 0.58, contact: 0.45}),
  daggers: Object.freeze({duration: 0.34, contact: 0.42}),
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
    /** Optional body-form movement personality (body-form-roster `motion`). */
    this.formMotion = null;
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

/**
 * Weapon-specific strike choreography. `strike` sweeps -1 (windup) to +1
 * (follow-through); `fwd` is its forward half, so anticipation and delivery
 * can be shaped independently while `amount` fades the whole action out.
 *
 * Weapon channels are HAND-local. With the arm raised forward by angle a,
 * the hand's -Z axis (forward at rest) tips up toward vertical while its -Y
 * axis (down at rest) tips forward. So: low-arm thrusts ride weaponZ,
 * raised-arm thrusts ride negative weaponY, and weapons that must stay level
 * take roughly -a of weaponPitch to counter the raise.
 */
function applyAttackPose(pose, weapon, t) {
  const amount = actionEnvelope(t);
  if (!amount) return;
  const strike = t < 0.28 ? -smooth(t / 0.28) : smooth((t - 0.28) / 0.34);
  const fwd = Math.max(0, strike);
  const coil = Math.max(0, -strike);
  switch (weapon) {
    case 'bow': {
      // Side-on archer: bow arm levels forward (bow pitch-countered to stay
      // vertical), string hand folds back to the cheek, snaps on release.
      const draw = t < 0.62 ? amount : Math.max(0, 1 - (t - 0.62) / 0.08);
      pose[P.bodyYaw] -= 0.38 * amount;
      pose[P.bodyRoll] -= 0.10 * draw;
      pose[P.armLPitch] += 1.45 * amount;
      pose[P.armRPitch] += 1.15 * amount;
      pose[P.elbowRPitch] -= 1.35 * draw;
      pose[P.headPitch] += 0.08 * draw;
      pose[P.weaponPitch] -= 1.45 * amount;
      pose[P.weaponY] += 0.25 * draw;
      pose[P.weaponYaw] += 0.22 * draw;
      break;
    }
    case 'spear': {
      // Low carry, wrist levels the shaft, then a long flat thrust off the
      // back foot with the whole body dropping behind it.
      pose[P.bodyYaw] += (0.34 * coil - 0.16 * fwd) * amount;
      pose[P.bodyPitch] += 0.30 * fwd * amount;
      pose[P.bodyY] -= 0.55 * fwd * amount;
      pose[P.armRPitch] += (0.30 + 0.25 * fwd) * amount;
      pose[P.elbowRPitch] += 0.30 * coil * amount;
      pose[P.armLPitch] += 0.45 * fwd * amount;
      pose[P.weaponPitch] -= (1.90 + 0.40 * fwd) * amount;
      pose[P.weaponZ] += (4.6 * fwd - 1.0 * coil) * amount;
      pose[P.weaponY] -= 1.6 * fwd * amount;
      pose[P.legLPitch] += 0.50 * fwd * amount;
      pose[P.legRPitch] -= 0.45 * fwd * amount;
      break;
    }
    case 'daggers': {
      // Three fast alternating reverse-grip jabs; the raised arms already
      // point the down-bladed daggers forward.
      const side = Math.sin(Math.max(0, t) * Math.PI * 3);
      pose[P.bodyYaw] += 0.38 * side * amount;
      pose[P.bodyPitch] += 0.18 * amount;
      pose[P.bodyY] -= 0.50 * amount;
      pose[P.armLPitch] += (0.95 - 0.40 * side) * amount;
      pose[P.armRPitch] += (0.95 + 0.40 * side) * amount;
      pose[P.elbowLPitch] += 0.45 * amount;
      pose[P.elbowRPitch] += 0.45 * amount;
      pose[P.armLRoll] -= 0.35 * amount;
      pose[P.armRRoll] += 0.35 * amount;
      pose[P.weaponRoll] += 1.25 * side * amount;
      pose[P.weaponY] -= 1.10 * Math.abs(side) * amount;
      break;
    }
    case 'staff': {
      // Rise while both arms lift the staff overhead, then whip the focus
      // forward-down as the burst lands.
      const charge = (t < 0.55 ? smooth(t / 0.55) : 1) * amount;
      const release = (t < 0.55 ? 0 : smooth((t - 0.55) / 0.18)) * amount;
      pose[P.bodyY] += 0.45 * charge - 0.90 * release;
      pose[P.bodyPitch] += 0.28 * release;
      pose[P.armLPitch] += 0.90 * charge + 0.30 * release;
      pose[P.armRPitch] += 1.05 * charge + 0.30 * release;
      pose[P.elbowLPitch] += 0.40 * charge;
      pose[P.elbowRPitch] += 0.40 * charge;
      pose[P.weaponY] += 0.60 * charge;
      pose[P.weaponPitch] -= 0.55 * charge + 1.65 * release;
      pose[P.corePulse] += 0.50 * charge + 0.50 * release;
      break;
    }
    case 'shield': {
      // Drop into a brace behind the shield, then drive it forward with the
      // whole body: a shoulder-led bash, not an arm wave.
      pose[P.bodyY] -= (0.55 * coil + 0.30 * fwd) * amount;
      pose[P.bodyYaw] += 0.25 * fwd * amount;
      pose[P.bodyPitch] += 0.34 * fwd * amount;
      pose[P.armLPitch] += (0.30 * coil + 0.45 * fwd) * amount;
      pose[P.elbowLPitch] += 0.50 * coil * amount;
      pose[P.weaponPitch] -= 0.55 * fwd * amount;
      pose[P.weaponY] -= 1.8 * fwd * amount;
      pose[P.weaponZ] += 1.2 * fwd * amount;
      pose[P.legLPitch] += 0.40 * fwd * amount;
      pose[P.legRPitch] -= 0.34 * fwd * amount;
      pose[P.kneeLPitch] += 0.25 * coil * amount;
      pose[P.kneeRPitch] += 0.25 * coil * amount;
      break;
    }
    case 'grapple': {
      // Raise and level the launcher, fire, and absorb the recoil backward
      // through the torso while the launcher kicks back along the arm.
      const aim = smooth(Math.min(1, t / 0.30));
      pose[P.armRPitch] += 1.35 * aim * amount;
      pose[P.elbowRPitch] += 0.10 * amount;
      pose[P.bodyYaw] -= 0.18 * amount;
      pose[P.bodyPitch] += (0.12 - 0.34 * fwd) * amount;
      pose[P.weaponPitch] -= 1.55 * aim * amount;
      pose[P.weaponY] += 0.90 * fwd * amount;
      pose[P.corePulse] += 0.35 * fwd * amount;
      break;
    }
    case 'sword':
    default: {
      // Overhead cleave: wind the shoulders back, then whip the blade over
      // the top with a torso twist and a short forward lunge.
      pose[P.bodyYaw] += 0.55 * strike * amount;
      pose[P.bodyPitch] += 0.22 * fwd * amount;
      pose[P.armRPitch] += (0.45 + 0.90 * fwd) * amount;
      pose[P.armRRoll] += 0.30 * strike * amount;
      pose[P.elbowRPitch] += 0.30 * coil * amount;
      pose[P.armLPitch] += 0.25 * amount;
      pose[P.weaponPitch] -= 2.70 * fwd * amount;
      pose[P.weaponRoll] += 0.40 * strike * amount;
      pose[P.weaponZ] += 0.60 * fwd * amount;
      pose[P.legLPitch] += 0.35 * fwd * amount;
      pose[P.legRPitch] -= 0.28 * fwd * amount;
      break;
    }
  }
}

/** Body-form movement personality layered over the shared biped gait. */
function applyFormFlavor(pose, state, moving, speed) {
  const form = state.formMotion;
  if (!form) return;
  const phase = state.gaitPhase;
  switch (form.flavor) {
    case 'hop':
      // Both legs launch together; the body arcs instead of striding.
      if (moving) {
        const hop = Math.max(0, Math.sin(phase * 0.5));
        const tuck = hop * 0.55;
        pose[P.bodyY] += hop * 2.2 * speed;
        pose[P.bodyPitch] += hop * 0.12;
        pose[P.legLPitch] = tuck;
        pose[P.legRPitch] = tuck;
        pose[P.kneeLPitch] = tuck * 0.9;
        pose[P.kneeRPitch] = tuck * 0.9;
        pose[P.armLPitch] = tuck * 0.4;
        pose[P.armRPitch] = tuck * 0.4;
      }
      break;
    case 'waddle':
      pose[P.bodyRoll] += Math.sin(phase) * (moving ? 0.16 : 0.03);
      break;
    case 'skitter':
      if (moving) {
        pose[P.bodyY] += Math.sin(phase * 2) * 0.22 * speed;
        pose[P.bodyRoll] += Math.sin(phase * 2.7) * 0.03;
      }
      break;
    case 'squash': {
      // Slimes travel as a pulse: compress, surge, repeat.
      const pulse = Math.sin(moving ? phase : state.elapsed * 2.4);
      pose[P.bodyY] += moving ? Math.abs(pulse) * 1.6 * speed : pulse * 0.25;
      if (moving) pose[P.bodyPitch] += pulse * 0.05;
      pose[P.corePulse] += 0.12 * pulse;
      break;
    }
    case 'flutter':
      if (moving) {
        const flap = Math.sin(phase * 2.4) * 0.50 * speed;
        pose[P.armLRoll] -= Math.abs(flap);
        pose[P.armRRoll] += Math.abs(flap);
        pose[P.bodyY] += Math.sin(phase * 2.4) * 0.18 * speed;
      }
      break;
    case 'lumber':
      pose[P.bodyYaw] += Math.sin(phase) * (moving ? 0.07 : 0.015);
      if (moving) pose[P.headPitch] -= Math.abs(Math.cos(phase)) * 0.05;
      break;
    case 'prowl':
      pose[P.bodyY] -= 0.3;
      if (moving) pose[P.bodyRoll] += Math.sin(phase) * 0.04;
      break;
    case 'glide':
      pose[P.bodyY] += Math.sin(phase * 0.5) * (moving ? 0.15 : 0.05);
      break;
    case 'rattle':
      pose[P.bodyRoll] += Math.sin(state.elapsed * 9) * 0.02;
      pose[P.headYaw] += Math.sin(state.elapsed * 7.3) * 0.05;
      break;
    default:
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

  const form = state.formMotion;
  const speed = clamp01(speedNow);
  const moving = movingNow === true && speed > 0.01;
  const restrained = reducedMotion === true;
  if (moving) {
    state.gaitPhase += step * profile.motion.strideHz * (form?.stride || 1)
      * TAU * (0.55 + speed * 0.65);
  }

  if (!restrained) {
    pose[P.bodyY] = Math.sin(state.elapsed * (1.1 + profile.motion.weight * 0.25) * TAU)
      * profile.motion.bob * (form?.bob || 1) * (moving ? 0.28 : 0.14);
    pose[P.bodyRoll] = Math.sin(state.elapsed * 1.7)
      * profile.motion.sway * (form?.sway || 1) * 0.18;
    pose[P.headYaw] = Math.sin(state.elapsed * 0.63) * 0.045;
  }

  if (moving) {
    const gait = Math.sin(state.gaitPhase);
    const legScale = Number.isFinite(form?.legScale) ? form.legScale : 1;
    const gaitScale = (restrained ? 0.36 : 1) * (0.42 + speed * 0.58) * legScale;
    pose[P.legLPitch] += gait * 0.62 * gaitScale;
    pose[P.legRPitch] -= gait * 0.62 * gaitScale;
    pose[P.kneeLPitch] += Math.max(0, -gait) * 0.52 * gaitScale;
    pose[P.kneeRPitch] += Math.max(0, gait) * 0.52 * gaitScale;
    pose[P.armLPitch] -= gait * 0.34 * gaitScale;
    pose[P.armRPitch] += gait * 0.34 * gaitScale;
    if (!restrained) {
      pose[P.bodyY] += Math.abs(Math.cos(state.gaitPhase))
        * profile.motion.bob * (form?.bob || 1) * 0.46 * gaitScale;
    }
  }

  if (!restrained) applyFormFlavor(pose, state, moving, speed);
  if (form?.posture) pose[P.bodyPitch] += form.posture;

  const attackT = advance(state, 'attackTimer', 'attackDuration', step);
  if (attackT >= 0) applyAttackPose(pose, state.attackType || profile.weapon, attackT);

  const shoveT = advance(state, 'shoveTimer', 'shoveDuration', step);
  if (shoveT >= 0) {
    const amount = actionEnvelope(shoveT);
    pose[P.bodyPitch] += 0.22 * amount;
    pose[P.armLPitch] += 0.92 * amount;
    pose[P.armRPitch] += 0.92 * amount;
    pose[P.elbowLPitch] += 0.20 * amount;
    pose[P.elbowRPitch] += 0.20 * amount;
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
    // Whiplash: head and torso snap BACKWARD away from the impact.
    const amount = (1 - hitT) * state.hitStrength;
    pose[P.headPitch] -= 0.42 * amount;
    pose[P.bodyPitch] -= 0.24 * amount;
  }

  if (state.woundLevel > 0 && alive) {
    // Wounded slump: hunch forward, chin down.
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

/**
 * Apply the sampled numeric pose to one Forge rig.
 *
 * This is the semantics -> rig-space boundary: forward-positive pitch and
 * thrust channels are negated exactly once here because the articulated
 * model's face points down local -Z.
 */
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
  const base = entry.basePose;
  j.body.position.y = base.bodyY + pose[P.bodyY];
  j.body.rotation.x = -(entry.profile.proportions.posture + pose[P.bodyPitch]);
  j.body.rotation.y = (base.bodyYaw || 0) + pose[P.bodyYaw];
  j.body.rotation.z = pose[P.bodyRoll];
  // Counter-rotate the head against most of the torso's forward pitch so a
  // ready-stance lean keeps the eyes on the target instead of reading as a
  // character about to tip over.
  j.head.rotation.x = (base.headPitch || 0) - pose[P.headPitch]
    + 0.6 * (entry.profile.proportions.posture + pose[P.bodyPitch]);
  j.head.rotation.y = pose[P.headYaw];
  j.leftArm.rotation.x = (base.armLPitch || 0) + pose[P.armLPitch];
  j.leftArm.rotation.z = base.armLRoll + pose[P.armLRoll];
  j.leftElbow.rotation.x = base.elbowLPitch + pose[P.elbowLPitch];
  j.rightArm.rotation.x = (base.armRPitch || 0) + pose[P.armRPitch];
  j.rightArm.rotation.z = base.armRRoll + pose[P.armRRoll];
  j.rightElbow.rotation.x = base.elbowRPitch + pose[P.elbowRPitch];
  j.leftLeg.rotation.x = pose[P.legLPitch];
  j.leftKnee.rotation.x = -(base.kneePitch + pose[P.kneeLPitch]);
  j.rightLeg.rotation.x = pose[P.legRPitch];
  j.rightKnee.rotation.x = -(base.kneePitch + pose[P.kneeRPitch]);

  const weaponNodes = entry.weaponPoseNodes || (entry.weapon ? [entry.weapon] : []);
  const weaponBases = entry.weaponBases || (entry.weaponBase ? [entry.weaponBase] : []);
  for (let index = 0; index < weaponNodes.length; index += 1) {
    const node = weaponNodes[index];
    const nodeBase = weaponBases[index];
    if (!node || !nodeBase) continue;
    node.position.x = nodeBase.x + pose[P.weaponX];
    node.position.y = nodeBase.y + pose[P.weaponY];
    node.position.z = nodeBase.z - pose[P.weaponZ];
    node.rotation.x = nodeBase.rx + pose[P.weaponPitch];
    node.rotation.y = nodeBase.ry + pose[P.weaponYaw];
    node.rotation.z = nodeBase.rz + pose[P.weaponRoll] * (nodeBase.sign || 1);
  }
  if (j.core) {
    const scale = Math.max(0.82, 1 + pose[P.corePulse]);
    j.core.scaling.setAll(scale);
  }
}
