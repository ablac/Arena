'use strict';

/**
 * Low-part-count weapons for the shared Forge character rig.
 *
 * Weapon materials are owned by the scene and reused by every bot. The only
 * per-bot material used here is the caller's mutable accent material, keeping
 * status flashes and cosmetic finish swaps compatible with BotRenderer.
 * @module renderer/forge-weapons
 */

const _sceneResources = new WeakMap();

function sharedMaterial(scene, name, diffuse, emissive, specular) {
  const B = window.BABYLON;
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = diffuse;
  material.emissiveColor = emissive;
  material.specularColor = specular;
  material.backFaceCulling = true;
  material.freeze();
  return material;
}

function getResources(scene) {
  let resources = _sceneResources.get(scene);
  if (resources) return resources;

  const B = window.BABYLON;
  resources = {
    steel: sharedMaterial(
      scene,
      'forge-weapon-steel',
      new B.Color3(0.38, 0.44, 0.52),
      new B.Color3(0.035, 0.05, 0.07),
      new B.Color3(0.62, 0.68, 0.76),
    ),
    dark: sharedMaterial(
      scene,
      'forge-weapon-dark',
      new B.Color3(0.045, 0.055, 0.075),
      new B.Color3(0.008, 0.012, 0.02),
      new B.Color3(0.22, 0.26, 0.32),
    ),
    cable: sharedMaterial(
      scene,
      'forge-weapon-cable',
      new B.Color3(0.10, 0.13, 0.17),
      new B.Color3(0.018, 0.025, 0.035),
      new B.Color3(0.16, 0.20, 0.24),
    ),
  };
  _sceneResources.set(scene, resources);
  return resources;
}

function finish(mesh, parent, material) {
  mesh.parent = parent;
  mesh.material = material;
  mesh.isPickable = false;
  return mesh;
}

function tipAnchor(root, name, position) {
  const B = window.BABYLON;
  const tip = new B.TransformNode(name, root.getScene());
  tip.parent = root;
  tip.position.copyFrom(position);
  root._trailTips.push(tip);
  return tip;
}

function buildSword(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const blade = finish(B.MeshBuilder.CreateCylinder(`forge-sword-blade-${id}`, {
    height: 11.5, diameterTop: 0.12, diameterBottom: 1.15, tessellation: 4,
  }, scene), root, materials.steel);
  blade.position.y = 6.25;

  const guard = finish(B.MeshBuilder.CreateBox(`forge-sword-guard-${id}`, {
    width: 3.8, height: 0.52, depth: 0.72,
  }, scene), root, accentMaterial);
  guard.position.y = 0.3;

  const grip = finish(B.MeshBuilder.CreateCylinder(`forge-sword-grip-${id}`, {
    height: 3.4, diameter: 0.72, tessellation: 6,
  }, scene), root, materials.dark);
  grip.position.y = -1.45;

  tipAnchor(root, `forge-sword-tip-${id}`, new B.Vector3(0, 12.05, 0));
  return [blade, guard, grip];
}

function buildBow(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const limbPath = [];
  for (let index = 0; index <= 10; index += 1) {
    const t = -1 + index / 5;
    limbPath.push(new B.Vector3(1.75 * (1 - t * t), t * 8.4, 0));
  }
  const limb = finish(B.MeshBuilder.CreateTube(`forge-bow-limb-${id}`, {
    path: limbPath, radius: 0.38, tessellation: 5, cap: B.Mesh.CAP_ALL,
  }, scene), root, accentMaterial);

  const stringPath = [
    limbPath[0].clone(),
    new B.Vector3(-1.0, 0, 0),
    limbPath[limbPath.length - 1].clone(),
  ];
  const string = finish(B.MeshBuilder.CreateTube(`forge-bow-string-${id}`, {
    path: stringPath, radius: 0.10, tessellation: 3, cap: B.Mesh.CAP_ALL,
  }, scene), root, materials.cable);

  const arrow = finish(B.MeshBuilder.CreateCylinder(`forge-bow-arrow-${id}`, {
    height: 10.5, diameterTop: 0.20, diameterBottom: 0.28, tessellation: 5,
  }, scene), root, materials.steel);
  arrow.rotation.x = Math.PI / 2;
  arrow.position.set(-0.7, 0, -2.4);
  tipAnchor(root, `forge-bow-tip-${id}`, new B.Vector3(-0.7, 0, -7.65));
  return [limb, string, arrow];
}

function buildSpear(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const shaft = finish(B.MeshBuilder.CreateCylinder(`forge-spear-shaft-${id}`, {
    height: 19.5, diameter: 0.62, tessellation: 6,
  }, scene), root, materials.dark);
  shaft.position.y = 5.5;

  const tip = finish(B.MeshBuilder.CreateCylinder(`forge-spear-tip-${id}`, {
    height: 4.8, diameterTop: 0.04, diameterBottom: 1.6, tessellation: 4,
  }, scene), root, materials.steel);
  tip.position.y = 17.65;

  const collar = finish(B.MeshBuilder.CreateCylinder(`forge-spear-collar-${id}`, {
    height: 1.25, diameterTop: 1.15, diameterBottom: 0.72, tessellation: 6,
  }, scene), root, accentMaterial);
  collar.position.y = 14.75;
  tipAnchor(root, `forge-spear-trail-tip-${id}`, new B.Vector3(0, 20.05, 0));
  return [shaft, tip, collar];
}

function buildDaggers(roots, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const meshes = [];
  for (const [index, side] of [-1, 1].entries()) {
    const root = roots[index];
    const blade = finish(B.MeshBuilder.CreateCylinder(`forge-dagger-blade-${id}-${side}`, {
      height: 5.6, diameterTop: 0.05, diameterBottom: 0.9, tessellation: 4,
    }, scene), root, materials.steel);
    blade.position.set(0, -3.0, 0);
    blade.rotation.z = side * 0.12;

    const grip = finish(B.MeshBuilder.CreateCylinder(`forge-dagger-grip-${id}-${side}`, {
      height: 2.4, diameter: 0.68, tessellation: 5,
    }, scene), root, side < 0 ? materials.dark : accentMaterial);
    grip.position.set(-side * 0.12, 0.85, 0);
    grip.rotation.z = side * 0.12;
    tipAnchor(root, `forge-dagger-tip-${id}-${side}`, new B.Vector3(side * 0.34, -5.8, 0));
    meshes.push(blade, grip);
  }
  return meshes;
}

function buildStaff(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const shaft = finish(B.MeshBuilder.CreateCylinder(`forge-staff-shaft-${id}`, {
    height: 19, diameter: 0.72, tessellation: 6,
  }, scene), root, materials.dark);
  shaft.position.y = 5.2;

  const focus = finish(B.MeshBuilder.CreateSphere(`forge-staff-focus-${id}`, {
    diameter: 2.8, segments: 6,
  }, scene), root, accentMaterial);
  focus.position.y = 15.7;

  const halo = finish(B.MeshBuilder.CreateTorus(`forge-staff-halo-${id}`, {
    diameter: 4.5, thickness: 0.34, tessellation: 12,
  }, scene), root, materials.steel);
  halo.position.y = 15.7;
  halo.rotation.x = Math.PI / 2 - 0.22;
  return [shaft, focus, halo];
}

function buildShield(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const shell = finish(B.MeshBuilder.CreateCylinder(`forge-shield-shell-${id}`, {
    height: 0.78, diameter: 8.6, tessellation: 8,
  }, scene), root, materials.dark);
  shell.position.y = 1.7;
  shell.rotation.x = Math.PI / 2;

  const rim = finish(B.MeshBuilder.CreateTorus(`forge-shield-rim-${id}`, {
    diameter: 8.7, thickness: 0.55, tessellation: 12,
  }, scene), root, materials.steel);
  rim.position.set(0, 1.7, -0.42);
  rim.rotation.x = Math.PI / 2;

  const boss = finish(B.MeshBuilder.CreateCylinder(`forge-shield-core-${id}`, {
    height: 0.92, diameterTop: 1.2, diameterBottom: 2.5, tessellation: 8,
  }, scene), root, accentMaterial);
  boss.position.set(0, 1.7, -0.65);
  boss.rotation.x = Math.PI / 2;
  return [shell, rim, boss];
}

function buildGrapple(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const launcher = finish(B.MeshBuilder.CreateBox(`forge-grapple-launcher-${id}`, {
    width: 2.7, height: 3.3, depth: 5.4,
  }, scene), root, materials.dark);
  launcher.position.z = -1.0;

  const spool = finish(B.MeshBuilder.CreateTorus(`forge-grapple-spool-${id}`, {
    diameter: 2.8, thickness: 0.48, tessellation: 10,
  }, scene), root, accentMaterial);
  spool.position.set(0, 0.1, -1.2);
  spool.rotation.x = Math.PI / 2;

  const cablePath = [
    new B.Vector3(0, 0.2, -3.5),
    new B.Vector3(0.4, 0.45, -5.8),
    new B.Vector3(0, 0.15, -8.2),
  ];
  const cable = finish(B.MeshBuilder.CreateTube(`forge-grapple-cable-${id}`, {
    path: cablePath, radius: 0.18, tessellation: 4, cap: B.Mesh.CAP_ALL,
  }, scene), root, materials.cable);

  const claw = finish(B.MeshBuilder.CreateCylinder(`forge-grapple-claw-${id}`, {
    height: 3.0, diameterTop: 0.08, diameterBottom: 1.8, tessellation: 4,
  }, scene), root, materials.steel);
  claw.rotation.x = Math.PI / 2;
  claw.position.set(0, 0.15, -9.65);
  tipAnchor(root, `forge-grapple-tip-${id}`, new B.Vector3(0, 0.15, -11.15));
  return [launcher, spool, cable, claw];
}

const BUILDERS = Object.freeze({
  sword: buildSword,
  bow: buildBow,
  spear: buildSpear,
  daggers: buildDaggers,
  staff: buildStaff,
  shield: buildShield,
  grapple: buildGrapple,
});

const REST_ROTATION = Object.freeze({
  sword: {x: 0.04, y: 0, z: -0.34},
  bow: {x: 0, y: -0.10, z: -0.08},
  spear: {x: 0.08, y: 0, z: -0.44},
  daggers: {x: 0, y: 0, z: 0},
  staff: {x: 0.04, y: 0, z: 0.10},
  shield: {x: 0, y: 0.14, z: -0.12},
  grapple: {x: -0.08, y: 0, z: 0},
});

/**
 * Construct one allowlisted Forge weapon and attach it to its semantic mount.
 * The returned TransformNode exposes normal getChildMeshes() for cosmetics.
 */
export function createForgeWeapon(profile, id, scene, mounts, accentMaterial, dimensions = {}) {
  const B = window.BABYLON;
  const type = BUILDERS[profile?.weapon] ? profile.weapon : 'sword';
  const root = new B.TransformNode(`forge-weapon-${type}-${id}`, scene);
  root._trailTips = [];

  const hand = profile?.weaponPose?.hand;
  root.parent = hand === 'left' ? mounts.handL : hand === 'both' ? mounts.chest : mounts.handR;

  const poseNodes = hand === 'both'
    ? [-1, 1].map((side) => {
        const node = new B.TransformNode(
          `forge-weapon-${type}-${side < 0 ? 'left' : 'right'}-${id}`,
          scene,
        );
        node.parent = side < 0 ? mounts.handL : mounts.handR;
        node._forgePoseSign = side;
        node._trailTips = [];
        return node;
      })
    : [root];
  root._forgePoseNodes = poseNodes;

  if (hand !== 'both') {
    root.position.set(
      (profile.weaponPose.restX || 0) * 2,
      (profile.weaponPose.restY || 0) * 2,
      (profile.weaponPose.restZ || 0) * 2,
    );
  }
  const rest = REST_ROTATION[type];
  root.rotation.set(rest.x, rest.y, rest.z);

  const materials = getResources(scene);
  const span = Math.max(3.0, Number(dimensions.handSpan) || 4.0);
  root._forgeMeshes = type === 'daggers'
    ? buildDaggers(poseNodes, id, scene, materials, accentMaterial)
    : BUILDERS[type](root, id, scene, materials, accentMaterial, span);
  if (type === 'daggers') {
    root._trailTips = poseNodes.flatMap(node => node._trailTips);
  }
  root._visibleMeshCount = root._forgeMeshes.length;
  return root;
}

/** Dispose nodes only; scene-owned and caller-owned materials remain owned. */
export function disposeForgeWeapon(weapon) {
  if (!weapon) return;
  for (const node of weapon._forgePoseNodes || []) {
    if (node !== weapon && typeof node.dispose === 'function' && !node.isDisposed()) {
      node.dispose();
    }
  }
  if (typeof weapon.dispose === 'function' && !weapon.isDisposed()) {
    weapon.dispose();
  }
}
