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
  'corePulse', 'footLPitch', 'footRPitch', 'hipYaw',
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
    this.locomotionWeight = 0;
    this.visualSpeed = 0;
    this.accelerationLean = 0;
    this.turnLean = 0;
    this.woundLevel = 0;
    this.targetRotY = 0;
    this.moveAngle = 0;
    this.wasAlive = true;
    /** Optional body-form movement personality (body-form-roster `motion`). */
    this.formMotion = null;
  }
}

function forgeAttackDuration(weapon, durationOverride) {
  const timing = Object.hasOwn(FORGE_ATTACK_TIMING, weapon)
    ? FORGE_ATTACK_TIMING[weapon] : FORGE_ATTACK_TIMING.sword;
  const override = Number(durationOverride);
  return Number.isFinite(override) && override > 0.16
    ? Math.min(override, 1.4)
    : timing.duration;
}

/** Delay effects until the Forge pose reaches its visible contact frame. */
export function forgeContactDelay(weapon, durationOverride) {
  const timing = Object.hasOwn(FORGE_ATTACK_TIMING, weapon)
    ? FORGE_ATTACK_TIMING[weapon] : FORGE_ATTACK_TIMING.sword;
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

/** Authored poses are offsets from each weapon's ready stance. Each score has
 * anticipation, contact and follow-through; timing is normalized to the same
 * contact landmark used by effects, including server cooldown overrides.
 * The seven scores carry their signature actions (cleave, draw, thrust/brace,
 * backstab, cast, bash and launch). There is no invented special-event clock.
 * Curves and channel lookup are compiled once, never allocated per bot/frame.
 */
function score(anticipation, contact, followThrough) {
  const channels = new Set([
    ...Object.keys(anticipation), ...Object.keys(contact), ...Object.keys(followThrough),
  ]);
  return Object.freeze([...channels].map((name) => Object.freeze([
    P[name], anticipation[name] || 0, contact[name] || 0, followThrough[name] || 0,
  ])));
}

const STRIKES = Object.freeze({
  // High guard rolls into a diagonal cleave; the hips resist the shoulder turn.
  sword: score(
    {
      bodyYaw: -0.48, hipYaw: 0.18, bodyY: -0.28, armRPitch: 1.52,
      armRRoll: 0.32, elbowRPitch: 0.7, armLPitch: 0.38, weaponPitch: -0.48,
      weaponRoll: -0.7, legRPitch: -0.18, kneeRPitch: 0.3,
    },
    {
      bodyYaw: 0.46, hipYaw: -0.16, bodyPitch: 0.23, bodyY: -0.58,
      armRPitch: 1.18, armRRoll: -0.2, elbowRPitch: 0.08, weaponPitch: -2.28,
      weaponRoll: 0.56, weaponZ: 0.5, legLPitch: 0.32, legRPitch: -0.32,
      kneeLPitch: 0.2,
    },
    {
      bodyYaw: 0.62, hipYaw: -0.22, bodyPitch: 0.3, bodyY: -0.42,
      armRPitch: 0.64, armRRoll: -0.3, weaponPitch: -2.7, weaponRoll: 0.85,
      legLPitch: 0.24, legRPitch: -0.22,
    }),
  // Side-on full draw, release at contact, then a small string-hand recoil.
  bow: score(
    {
      bodyYaw: -0.48, hipYaw: 0.22, armLPitch: 1.4, armRPitch: 1.1,
      elbowRPitch: -1.1, armRRoll: 0.2, weaponPitch: -1.4, headYaw: 0.26,
      bodyY: -0.12, legLPitch: 0.16, legRPitch: -0.16,
    },
    {
      bodyYaw: -0.48, hipYaw: 0.22, armLPitch: 1.4, armRPitch: 1.18,
      elbowRPitch: -1.32, armRRoll: 0.28, weaponPitch: -1.4, headYaw: 0.26,
      bodyY: -0.16, legLPitch: 0.16, legRPitch: -0.16,
    },
    {
      bodyYaw: -0.39, hipYaw: 0.19, armLPitch: 1.38, armRPitch: 0.76,
      elbowRPitch: -0.66, armRRoll: 0.48, weaponPitch: -1.38, headYaw: 0.24,
      bodyPitch: -0.04,
    }),
  // Rear-leg load into a level thrust that stays braced before withdrawal.
  spear: score(
    {
      bodyYaw: 0.4, hipYaw: -0.18, bodyY: -0.3, armRPitch: 0.25,
      elbowRPitch: 0.65, armLPitch: 0.55, weaponPitch: -1.7, weaponZ: -1.2,
      legLPitch: 0.1, legRPitch: -0.18, kneeRPitch: 0.36,
    },
    {
      bodyYaw: -0.22, hipYaw: 0.12, bodyPitch: 0.26, bodyY: -0.68,
      armRPitch: 0.62, elbowRPitch: -0.1, armLPitch: 0.7, weaponPitch: -2.2,
      weaponZ: 3.4, weaponY: -1.1, legLPitch: 0.48, legRPitch: -0.4,
      kneeLPitch: 0.22,
    },
    {
      bodyYaw: -0.26, hipYaw: 0.13, bodyPitch: 0.29, bodyY: -0.72,
      armRPitch: 0.65, armLPitch: 0.7, weaponPitch: -2.23, weaponZ: 3.7,
      weaponY: -1.15, legLPitch: 0.5, legRPitch: -0.43, kneeLPitch: 0.26,
    }),
  // Low guard and a single committed crossing jab with the other hand guarding.
  daggers: score(
    {
      bodyY: -0.65, bodyPitch: 0.18, bodyYaw: -0.34, hipYaw: 0.16,
      armLPitch: 1.1, armRPitch: 0.45, elbowLPitch: 0.4, elbowRPitch: 0.5,
      armLRoll: -0.2, armRRoll: 0.24, kneeLPitch: 0.25, kneeRPitch: 0.25,
    },
    {
      bodyY: -0.82, bodyPitch: 0.26, bodyYaw: 0.4, hipYaw: -0.18,
      armLPitch: 0.55, armRPitch: 1.5, elbowLPitch: 0.48, elbowRPitch: -0.08,
      weaponRoll: 0.44, weaponY: -0.55, legLPitch: 0.34, legRPitch: -0.26,
    },
    {
      bodyY: -0.64, bodyPitch: 0.22, bodyYaw: 0.5, hipYaw: -0.2,
      armLPitch: 0.66, armRPitch: 1.1, elbowRPitch: 0.1, weaponRoll: 0.75,
      legLPitch: 0.3, legRPitch: -0.2,
    }),
  // Raised focus gathers energy, then the caster drops their weight into the cast.
  staff: score(
    {
      bodyY: 0.3, bodyPitch: -0.08, bodyYaw: -0.28, hipYaw: 0.14,
      armRPitch: 1.65, armLPitch: 0.8, elbowRPitch: 0.42, armLRoll: -0.48,
      weaponPitch: -0.8, weaponY: 0.5, corePulse: 0.38,
    },
    {
      bodyY: -0.62, bodyPitch: 0.24, bodyYaw: 0.24, hipYaw: -0.12,
      armRPitch: 1.1, armLPitch: 1.2, elbowRPitch: 0.08, weaponPitch: -2.12,
      weaponY: -0.5, corePulse: 0.75, kneeLPitch: 0.27, kneeRPitch: 0.27,
    },
    {
      bodyY: -0.7, bodyPitch: 0.28, bodyYaw: 0.32, hipYaw: -0.16,
      armRPitch: 0.95, armLPitch: 1.15, weaponPitch: -2.4, corePulse: 0.28,
      kneeLPitch: 0.3, kneeRPitch: 0.3,
    }),
  // Compact brace followed by a shoulder-led bash with the back leg driving.
  shield: score(
    {
      bodyY: -0.58, bodyYaw: -0.22, hipYaw: 0.12, bodyPitch: 0.08,
      armLPitch: 0.2, elbowLPitch: 0.52, armRPitch: 0.5, kneeLPitch: 0.32,
      kneeRPitch: 0.32, weaponZ: -0.25,
    },
    {
      bodyY: -0.65, bodyYaw: 0.3, hipYaw: -0.14, bodyPitch: 0.32,
      armLPitch: 0.65, elbowLPitch: 0.08, armRPitch: 0.65, weaponPitch: -0.65,
      weaponY: -1.35, weaponZ: 0.9, legLPitch: 0.36, legRPitch: -0.34,
    },
    {
      bodyY: -0.5, bodyYaw: 0.38, hipYaw: -0.18, bodyPitch: 0.34,
      armLPitch: 0.67, weaponPitch: -0.67, weaponY: -1.45, weaponZ: 1,
      legLPitch: 0.38, legRPitch: -0.34,
    }),
  // Level aim, launch, then the rear knee absorbs the cable launcher recoil.
  grapple: score(
    {
      bodyYaw: -0.22, hipYaw: 0.12, armRPitch: 1.3, elbowRPitch: 0.18,
      armLPitch: 0.65, weaponPitch: -1.5, bodyY: -0.18, legLPitch: 0.14,
      legRPitch: -0.14,
    },
    {
      bodyYaw: -0.2, hipYaw: 0.1, armRPitch: 1.35, elbowRPitch: 0.05,
      armLPitch: 0.7, weaponPitch: -1.55, bodyPitch: 0.08, bodyY: -0.24,
      corePulse: 0.35,
    },
    {
      bodyYaw: -0.35, hipYaw: 0.18, armRPitch: 1.16, elbowRPitch: 0.38,
      armLPitch: 0.62, weaponPitch: -1.4, weaponY: 0.65, bodyPitch: -0.16,
      bodyY: -0.4, kneeRPitch: 0.32,
    }),
});


function applyAttackPose(pose, weapon, t) {
  const key = Object.hasOwn(STRIKES, weapon) ? weapon : 'sword';
  const contact = FORGE_ATTACK_TIMING[key].contact;
  const ready = contact * 0.64;
  const settle = contact + (1 - contact) * 0.24;
  let from, to, blend;
  if (t < ready) { from = 0; to = 1; blend = smooth(t / ready); }
  else if (t < contact) { from = 1; to = 2; blend = smooth((t - ready) / (contact - ready)); }
  else if (t < settle) { from = 2; to = 3; blend = smooth((t - contact) / (settle - contact)); }
  else {
    from = 3; to = 0;
    const recovery = clamp01((t - settle) / (1 - settle));
    // Recover most of the reach early, then ease the heavy joints back into
    // guard. Endpoints and all anticipation/contact/follow-through keys stay
    // exact; no new pose offset can leak into the next attack.
    blend = smooth(1 - (1 - recovery) * (1 - recovery));
  }
  const frames = STRIKES[key];
  for (const channel of frames) {
    const start = from ? channel[from] : 0;
    const end = to ? channel[to] : 0;
    pose[channel[0]] += start + (end - start) * blend;
  }
}

/** Secondary body-form personality, restrained enough to preserve footwork. */
function applyFormFlavor(pose, state, moving, speed) {
  const form = state.formMotion;
  if (!form) return;
  const phase = state.gaitPhase;
  const pulse = Math.sin(phase);
  const lift = Math.sin(phase * 2);
  const amount = moving ? speed : 0.12;
  switch (form.flavor) {
    case 'hop':
      pose[P.bodyY] += (1 - Math.cos(phase * 2)) * 0.24 * amount;
      pose[P.kneeLPitch] += Math.max(0, lift) * 0.22 * amount;
      pose[P.kneeRPitch] += Math.max(0, lift) * 0.22 * amount;
      break;
    case 'waddle': pose[P.bodyRoll] += pulse * 0.095 * amount; break;
    case 'skitter': pose[P.bodyPitch] += 0.07 * amount; break;
    case 'squash':
      pose[P.bodyY] += lift * 0.35 * amount;
      pose[P.corePulse] += lift * 0.06 * amount;
      break;
    case 'flutter':
      pose[P.armLRoll] -= (0.15 + lift * 0.1) * amount;
      pose[P.armRRoll] += (0.15 + lift * 0.1) * amount;
      break;
    case 'lumber': pose[P.hipYaw] += pulse * 0.065 * amount; break;
    case 'prowl': pose[P.bodyY] -= 0.22; break;
    case 'glide': pose[P.bodyY] += Math.sin(phase * 0.5) * 0.08 * amount; break;
    case 'rattle': pose[P.headYaw] += Math.sin(state.elapsed * 3.2) * 0.025; break;
    default: break;
  }
}

// A loaded foot traverses the stance at constant speed; only the unloaded
// foot eases through its return. Heel strike and toe release are brief ankle
// rotations, not whole-leg kicks. A 62% stance supplies double support.
function applyStep(pose, phase, scale, left, carry) {
  const stance = phase < 0.62;
  const u = stance ? phase / 0.62 : (phase - 0.62) / 0.38;
  const stride = stance ? 1 - 2 * u : -1 + 2 * smooth(u);
  const lift = stance ? 0 : Math.sin(Math.PI * u) ** 2;
  const knee = (stance ? 0.1 * Math.sin(Math.PI * u) : lift * 0.92) * scale;
  const leg = stride * 0.46 * scale;
  const ankle = stance
    ? (-0.2 * (1 - smooth(u / 0.16)) + 0.34 * smooth((u - 0.78) / 0.22)) * scale
    : (0.34 - 0.54 * smooth(u) - 0.16 * lift) * scale;
  pose[left ? P.legLPitch : P.legRPitch] += leg;
  pose[left ? P.kneeLPitch : P.kneeRPitch] += knee;
  // Counter the shin orientation so the loaded sole stays close to level.
  pose[left ? P.footLPitch : P.footRPitch] += -leg + knee + ankle;
  pose[left ? P.armLPitch : P.armRPitch] -= stride * 0.23 * scale * carry;
  pose[left ? P.elbowLPitch : P.elbowRPitch] += (0.09 + lift * 0.13) * scale * carry;
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
 * @param {boolean} secondaryMotion optional weight shifts and form flavor
 * @param {number} travelDistance rendered world distance, or -1 for preview cadence
 */
export function sampleForgePose(
  profile, state, dt,
  movingNow = false, speedNow = 0, alive = true, reducedMotion = false,
  secondaryMotion = true, travelDistance = -1,
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
    state.hitTimer = -1;
    state.respawnTimer = -1;
  } else if (alive && !state.wasAlive) {
    state.deathTimer = -1;
    state.respawnTimer = 0;
  }
  state.wasAlive = alive;

  const form = state.formMotion;
  const speed = clamp01(speedNow);
  const moving = movingNow === true && speed > 0.01;
  const restrained = reducedMotion === true;
  const secondary = !restrained && secondaryMotion;
  const blend = 1 - Math.exp(-step * 12);
  state.locomotionWeight += ((moving ? 1 : 0) - state.locomotionWeight) * blend;
  const previousSpeed = state.visualSpeed;
  state.visualSpeed += (speed - state.visualSpeed) * blend;
  // Weight shifts into acceleration and back over the heels on braking.
  // Filtered speed keeps snapshot jitter from shaking the torso.
  const acceleration = step > 0 ? (state.visualSpeed - previousSpeed) / step : 0;
  state.accelerationLean += (Math.max(-0.12, Math.min(0.16, acceleration * 0.045))
    - state.accelerationLean) * (1 - Math.exp(-step * 9));
  if (moving) {
    // Real-world travel drives live gait. Preview samplers which have no
    // position input retain a speed-driven cycle with the same class flavor.
    const cycle = travelDistance >= 0
      ? travelDistance / (38 * profile.proportions.leg)
      : step * profile.motion.strideHz * (0.55 + speed * 0.65);
    state.gaitPhase = (state.gaitPhase + Math.min(cycle, step * 4) * (form?.stride || 1) * TAU) % TAU;
  }

  if (secondary) {
    // Slow expansion and an offset shoulder settle avoid a synchronized idle
    // metronome. Disabling secondary motion or enabling reduced motion
    // suppresses this idle layer completely.
    const breath = Math.sin(state.elapsed * 1.65);
    const rest = 1 - state.locomotionWeight;
    pose[P.bodyY] = breath * 0.085 * rest;
    pose[P.bodyPitch] = breath * 0.008 * rest;
    pose[P.bodyRoll] = Math.sin(state.elapsed * 0.73) * 0.012 * rest;
    pose[P.headYaw] = Math.sin(state.elapsed * 0.41) * 0.026 * rest;
    pose[P.armLPitch] = breath * 0.013 * rest;
    pose[P.armRPitch] = -breath * 0.009 * rest;
  }

  if (state.locomotionWeight > 0.0001) {
    const phase = ((state.gaitPhase / TAU) % 1 + 1) % 1;
    const legScale = Number.isFinite(form?.legScale) ? form.legScale : 1;
    const gaitScale = (restrained ? 0.36 : 1)
      * (0.35 + state.visualSpeed * 0.65) * legScale * state.locomotionWeight;
    const carry = state.attackTimer >= 0 ? 0.15 : 1;
    applyStep(pose, phase, gaitScale, true, carry);
    applyStep(pose, (phase + 0.5) % 1, gaitScale, false, carry);
    if (secondary) {
      const load = Math.sin(state.gaitPhase);
      pose[P.bodyY] += (Math.cos(state.gaitPhase * 2) - 1) * 0.21 * gaitScale;
      pose[P.bodyRoll] += load * 0.026 * gaitScale;
      pose[P.bodyYaw] -= load * 0.075 * gaitScale * carry;
      pose[P.hipYaw] += load * 0.13 * gaitScale;
      pose[P.headYaw] += load * 0.035 * gaitScale * carry;
      pose[P.bodyPitch] += state.visualSpeed * (0.045 + profile.motion.weight * 0.02);
    }
  }

  if (secondary) {
    pose[P.bodyPitch] += state.accelerationLean;
    pose[P.bodyRoll] += state.turnLean;
    pose[P.headYaw] -= state.turnLean * 0.65;
    applyFormFlavor(pose, state, moving, speed * state.locomotionWeight);
  }
  if (form?.posture) pose[P.bodyPitch] += form.posture;

  const attackT = advance(state, 'attackTimer', 'attackDuration', step);
  if (attackT >= 0) applyAttackPose(pose, state.attackType || profile.weapon, attackT);

  const shoveT = advance(state, 'shoveTimer', 'shoveDuration', step);
  if (shoveT >= 0) {
    const brace = actionEnvelope(shoveT);
    const drive = smooth((shoveT - 0.22) / 0.22) * (1 - smooth((shoveT - 0.56) / 0.44));
    pose[P.bodyY] -= 0.48 * brace;
    pose[P.bodyPitch] += 0.3 * drive;
    pose[P.armLPitch] += 0.45 * brace + 0.7 * drive;
    pose[P.armRPitch] += 0.45 * brace + 0.7 * drive;
    pose[P.elbowLPitch] += 0.42 * (brace - drive);
    pose[P.elbowRPitch] += 0.42 * (brace - drive);
    pose[P.legLPitch] += 0.28 * drive;
    pose[P.legRPitch] -= 0.26 * drive;
    pose[P.kneeLPitch] += 0.2 * brace;
    pose[P.weaponZ] += 0.75 * drive;
  }

  const dodgeT = advance(state, 'dodgeTimer', 'dodgeDuration', step);
  if (dodgeT >= 0) {
    const load = smooth(dodgeT / 0.18) * (1 - smooth((dodgeT - 0.55) / 0.45));
    const flight = Math.sin(Math.PI * clamp01((dodgeT - 0.16) / 0.7));
    const side = Math.sin(state.dodgeAngle);
    const forward = Math.cos(state.dodgeAngle);
    pose[P.bodyY] -= 0.8 * load;
    pose[P.bodyPitch] += (0.16 + 0.14 * forward) * load;
    pose[P.bodyRoll] += side * 0.36 * flight;
    pose[P.hipYaw] -= side * 0.12 * flight;
    pose[P.armLPitch] += 0.3 * load;
    pose[P.armRPitch] += 0.3 * load;
    pose[P.legLPitch] += (0.32 * forward - 0.25 * side) * flight;
    pose[P.legRPitch] -= (0.32 * forward + 0.25 * side) * flight;
    pose[P.kneeLPitch] += (0.26 + 0.2 * Math.max(0, side)) * load;
    pose[P.kneeRPitch] += (0.26 + 0.2 * Math.max(0, -side)) * load;
  }

  const hitT = advance(state, 'hitTimer', 'hitDuration', step);
  if (hitT >= 0) {
    const recoil = (1 - smooth(hitT)) * state.hitStrength;
    const catchWeight = Math.sin(Math.PI * hitT) * state.hitStrength;
    pose[P.headPitch] -= 0.32 * recoil;
    pose[P.bodyPitch] -= 0.2 * recoil;
    pose[P.bodyYaw] += 0.09 * recoil;
    pose[P.bodyY] -= 0.3 * catchWeight;
    pose[P.kneeRPitch] += 0.16 * catchWeight;
    pose[P.elbowRPitch] += 0.15 * recoil;
  }

  if (state.woundLevel > 0 && alive) {
    // Wounded slump: hunch forward, chin down.
    pose[P.bodyPitch] += state.woundLevel * 0.055;
    pose[P.headPitch] += state.woundLevel * 0.035;
  }

  const deathT = advance(state, 'deathTimer', 'deathDuration', step, true);
  if (deathT >= 0) {
    // Knees give first, then the torso tips, then the limbs settle. Death is
    // an absolute layer: an old stride cannot keep kicking the fallen body.
    pose.fill(0);
    const buckle = smooth(deathT / 0.38);
    const fall = smooth((deathT - 0.2) / 0.63);
    const settle = smooth((deathT - 0.76) / 0.24);
    pose[P.bodyY] = -1.1 * buckle - 6.4 * fall;
    pose[P.bodyPitch] = 0.24 * buckle + 0.38 * fall;
    pose[P.bodyRoll] = 1.16 * fall;
    pose[P.headPitch] = 0.18 * buckle + 0.1 * settle;
    pose[P.hipYaw] = -0.18 * fall;
    pose[P.kneeLPitch] = 0.65 * buckle - 0.16 * settle;
    pose[P.kneeRPitch] = 0.85 * buckle - 0.24 * settle;
    pose[P.legLPitch] = 0.24 * fall;
    pose[P.legRPitch] = -0.2 * fall;
    pose[P.armLPitch] = 0.5 * fall - 0.12 * settle;
    pose[P.armRPitch] = -0.25 * fall;
    pose[P.armLRoll] = -0.2 * fall;
    pose[P.elbowRPitch] = 0.35 * fall;
  }

  const respawnT = advance(state, 'respawnTimer', 'respawnDuration', step);
  if (respawnT >= 0) {
    const rise = 1 - smooth(respawnT);
    pose[P.bodyY] -= rise * 3.8;
    pose[P.bodyPitch] += rise * 0.28;
    pose[P.kneeLPitch] += rise * 0.65;
    pose[P.kneeRPitch] += rise * 0.65;
    pose[P.armLPitch] += rise * 0.38;
    pose[P.armRPitch] += rise * 0.38;
  }

  if (secondary && alive) pose[P.corePulse] += Math.sin(state.elapsed * 1.65) * 0.035;
  // Protect the render boundary from non-finite caller data and stacked
  // reactions. Translation and rotation envelopes are deliberately bounded.
  for (let index = 0; index < pose.length; index += 1) {
    const limit = index === P.bodyY ? 10 : (index >= P.weaponX && index <= P.weaponZ ? 8 : 3.2);
    pose[index] = Number.isFinite(pose[index]) ? Math.max(-limit, Math.min(limit, pose[index])) : 0;
  }
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
export function updateForgeCharacter(
  entry, dt, reducedMotion = false, highDetail = true, secondaryMotion = true,
) {
  if (!entry?.joints || !entry.anim) return;
  const step = Math.max(0, Math.min(Number.isFinite(dt) ? dt : 0, 0.1));
  const root = entry.root;
  const lastX = Number.isFinite(entry._poseX) ? entry._poseX : root.position.x;
  const lastZ = Number.isFinite(entry._poseZ) ? entry._poseZ : root.position.z;
  const dx = root.position.x - lastX;
  const dz = root.position.z - lastZ;
  entry._poseX = root.position.x;
  entry._poseZ = root.position.z;
  const distance = Math.hypot(dx, dz);
  // A teleport/respawn moves the gameplay root without producing a sprint.
  // Also consume position changes on a paused frame without turning them
  // into a velocity spike when rendering resumes.
  const travel = step > 0 && Number.isFinite(distance) && distance <= Math.max(12, step * 600)
    ? distance : 0;
  const speed = Math.min(1, travel / Math.max(0.001, step * 100));
  const moving = travel > step * 0.25;
  if (moving && entry.anim.attackTimer < 0) {
    entry.anim.targetRotY = Math.atan2(dx, dz);
    entry.anim.moveAngle = entry.anim.targetRotY;
  }
  const turn = shortestAngle(root.rotation.y, entry.anim.targetRotY);
  root.rotation.y += turn * (1 - Math.exp(-step * 10));
  const turnTarget = moving ? Math.max(-0.13, Math.min(0.13, turn * speed * 0.18)) : 0;
  entry.anim.turnLean += (turnTarget - entry.anim.turnLean) * (1 - Math.exp(-step * 9));

  const pose = sampleForgePose(
    entry.profile, entry.anim, step,
    moving, speed, entry.isAlive, reducedMotion, secondaryMotion, travel,
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
  if (j.leftFoot) j.leftFoot.rotation.x = (base.footLPitch || 0) + pose[P.footLPitch];
  if (j.rightFoot) j.rightFoot.rotation.x = (base.footRPitch || 0) + pose[P.footRPitch];
  if (j.hips) j.hips.rotation.y = (base.hipYaw || 0) + pose[P.hipYaw];

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
