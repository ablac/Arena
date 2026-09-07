'use strict';

/**
 * Authored, articulated full-body silhouettes. Geometry is constructed once
 * per scene and shared by clones; each character owns only three mutable
 * materials and its declared number of draw meshes. The profiles below are
 * sculpted cross sections, not boxes hidden under cosmetic attachments.
 * Coordinates face -Z; body-local y=0 is the hip and the floor is -bodyY.
 * @module renderer/body-form-geometry
 */
import {makeMat, parseColor} from './utils.js';

const _nearResources = new WeakMap();
const _farResources = new WeakMap();

function vector(value, fallback) {
  return Number.isFinite(Number(value)) && Number(value) > 0 ? Number(value) : fallback;
}

// Three coherent surface roles per form: shell, contrast, and face/detail.
// Organic characters need dark eyes rather than neon featureless faces.
const PALETTES = Object.freeze({
  giant_chicken: ['#eee3ca', '#bb472a', '#29241f'],
  highland_cow: ['#925434', '#d3a16b', '#262422'],
  corgi: ['#c7793f', '#f2ddba', '#282731'],
  tabby_cat: ['#807367', '#cfbca0', '#292c2b'],
  red_fox: ['#b9522d', '#e9d8b4', '#302a29'],
  battle_rabbit: ['#a9a7b2', '#e8e2db', '#44313b'],
  emperor_penguin: ['#202c3b', '#e9e5d9', '#d5a343'],
  bullfrog: ['#4e8050', '#aac779', '#263a2b'],
  land_shark: ['#567a8f', '#d5ded8', '#253244'],
  tyrant_rex: ['#607d49', '#b5bc79', '#30352d'],
  human_adventurer: ['#b68160', '#344c62', '#322d2b'],
  astronaut: ['#dae1e2', '#425568', '#183d4c'],
  knight: ['#8a9ca8', '#394753', '#b69957'],
  wizard: ['#48415f', '#b5a68e', '#82b9c3'],
  skeleton: ['#d1c9b5', '#333b43', '#84c2c6'],
  stone_golem: ['#6e7977', '#434f51', '#d69e59'],
  slime_monarch: ['#51a88a', '#245d57', '#d9b66b'],
  spider_drone: ['#303d4b', '#7e8b94', '#bd789d'],
});

function makeMaterials(spec, scene, id) {
  const colors = PALETTES[spec.key] || [spec.primary, spec.secondary, spec.accent];
  const luminous = ['wizard', 'stone_golem', 'skeleton', 'spider_drone'].includes(spec.key);
  const all = colors.map((color, index) => {
    const material = makeMat(`body-form-${spec.key}-${index}-${id}`, scene, parseColor(color), {
      emissiveFactor: index === 2 && luminous ? 0.16 : 0.015,
      specular: parseColor(index === 2 ? '#637579' : '#343b3e'),
    });
    material.specularPower = spec.family === 'slime' ? 96 : 34;
    material.backFaceCulling = true;
    material._forgeRestEmissive = material.emissiveColor.clone();
    return material;
  });
  return {primary: all[0], secondary: all[1], accent: all[2], all};
}

// y, width, depth, optional x/z center. Smooth normals join the contour rings.
const PROFILES = Object.freeze({
  torso: [[-.5,.62,.62],[-.42,.8,.76],[-.18,.74,.77],[.08,.86,.91],[.30,1,.93],[.44,.82,.7],[.5,.42,.48]],
  pear: [[-.5,.48,.5],[-.43,.82,.75],[-.25,1,.95],[0,.99,1],[.25,.78,.88],[.43,.48,.6],[.5,.2,.34]],
  barrel: [[-.5,.64,.7],[-.38,.91,.95],[-.1,1,1],[.22,.97,.96],[.4,.84,.83],[.5,.6,.6]],
  chest: [[-.5,.44,.6],[-.25,.68,.85],[.1,1,1],[.32,.95,.85],[.5,.56,.6]],
  limb: [[-.5,.6,.62],[-.43,.83,.85],[-.22,.92,1],[.10,1,.98],[.35,.92,.83],[.5,.7,.72]],
  bone: [[-.5,.88,.88],[-.4,1,1],[-.22,.54,.54],[.22,.54,.54],[.4,1,1],[.5,.88,.88]],
  boot: [[-.5,.93,1,0,-.15],[-.3,1,1,0,-.15],[0,.86,.78,0,-.12],[.2,.65,.52,0,.10],[.5,.6,.48,0,.14]],
  ear: [[-.5,.82,.8],[-.3,1,1],[.03,.77,.73],[.32,.44,.37],[.5,.025,.025]],
  longEar: [[-.5,.6,.64],[-.2,.82,.82],[.15,1,1],[.39,.76,.76],[.5,.12,.15]],
  fin: [[-.5,.94,1],[-.25,1,.73,0,.03],[.05,.8,.5,0,.16],[.3,.42,.24,0,.32],[.5,.015,.025,0,.5]],
  horn: [[-.5,1,1],[-.25,.85,.83,.10,0],[0,.58,.57,.24,.02],[.3,.28,.25,.38,.06],[.5,.01,.01,.39,.1]],
  tail: [[-.5,.54,.6],[-.26,.92,.93,0,.1],[0,1,1,0,.22],[.23,.82,.83,0,.31],[.43,.42,.4,0,.32],[.5,.025,.025,0,.29]],
  robe: [[-.5,1,1],[-.45,.98,.98],[-.16,.80,.77],[.17,.56,.58],[.32,.65,.62],[.45,.53,.52],[.5,.31,.37]],
  hat: [[-.5,1,1],[-.33,.78,.78],[-.03,.56,.58,.05,0],[.22,.33,.4,.16,0],[.4,.14,.2,.32,.03],[.5,.02,.02,.48,.1]],
  jaw: [[-.5,.45,.5],[-.32,.8,.8],[0,1,1],[.3,.94,.95],[.5,.7,.72]],
  rock: [[-.5,.64,.7],[-.30,.92,.89],[-.05,1,1,.04,0],[.24,.91,.96,-.03,.04],[.5,.62,.64,.03,0]],
});

/** Closed elliptical ring surface. Authored recipes are finite local constants. */
function sculpt(B, name, rings, scene, facets = 20) {
  const positions = [], indices = [], normals = [], uvs = [];
  for (let row = 0; row < rings.length; row += 1) {
    const [y, w, d, x = 0, z = 0] = rings[row];
    for (let column = 0; column < facets; column += 1) {
      const angle = column / facets * Math.PI * 2;
      positions.push(x + Math.cos(angle) * w / 2, y, z + Math.sin(angle) * d / 2);
      uvs.push(column / facets, row / (rings.length - 1));
      if (row < rings.length - 1) {
        const a = row * facets + column, b = row * facets + (column + 1) % facets;
        // Babylon's left-handed front faces use clockwise winding.
        indices.push(a, b, a + facets, b, b + facets, a + facets);
      }
    }
  }
  // Caps use independent vertices so the sole and crown stay genuinely flat.
  for (const row of [0, rings.length - 1]) {
    const [y, w, d, x = 0, z = 0] = rings[row];
    const center = positions.length / 3;
    positions.push(x, y, z); uvs.push(.5, .5);
    for (let column = 0; column < facets; column += 1) {
      const angle = column / facets * Math.PI * 2;
      positions.push(x + Math.cos(angle) * w / 2, y, z + Math.sin(angle) * d / 2);
      uvs.push(.5 + Math.cos(angle) / 2, .5 + Math.sin(angle) / 2);
    }
    for (let column = 0; column < facets; column += 1) {
      const a = center + 1 + column, b = center + 1 + (column + 1) % facets;
      indices.push(...(row === 0 ? [center, b, a] : [center, a, b]));
    }
  }
  const mesh = new B.Mesh(name, scene);
  const data = new B.VertexData();
  B.VertexData.ComputeNormals(positions, indices, normals);
  Object.assign(data, {positions, indices, normals, uvs});
  data.applyToMesh(mesh);
  return mesh;
}

/** Eyes and nose are one reusable facial mesh, with proper round contours. */
function faceTemplate(B, shape, name, scene) {
  const pieces = [];
  const features = [[-.23,.065,-.447,.105,.12,.065],[.23,.065,-.447,.105,.12,.065]];
  if (shape === 'face') features.push([0,-.20,-.665,.145,.10,.105]);
  if (shape === 'skullFace') features.splice(0, features.length,
    [-.23,.04,-.43,.28,.28,.16],[.23,.04,-.43,.28,.28,.16],[0,-.2,-.48,.13,.18,.08]);
  for (const [index, feature] of features.entries()) {
    const mesh = B.MeshBuilder.CreateSphere(`${name}-${index}`, {diameter: 1, segments: 12}, scene);
    mesh.position.set(...feature.slice(0, 3)); mesh.scaling.set(...feature.slice(3));
    pieces.push(mesh);
  }
  const mesh = B.Mesh.MergeMeshes(pieces, true, true);
  if (!mesh) throw new Error(`Unable to construct ${shape}`);
  mesh.name = name;
  return mesh;
}

function nearPrimitiveTemplate(B, shape, scene) {
  let resources = _nearResources.get(scene);
  if (!resources) { resources = new Map(); _nearResources.set(scene, resources); }
  if (resources.has(shape)) return resources.get(shape);
  const name = `body-form-near-template-${shape}`;
  let template;
  if (PROFILES[shape]) template = sculpt(B, name, PROFILES[shape], scene, shape === 'rock' ? 7 : 20);
  else if (['face', 'eyes', 'skullFace'].includes(shape)) template = faceTemplate(B, shape, name, scene);
  else if (shape === 'torus') template = B.MeshBuilder.CreateTorus(name, {diameter: 1, thickness: .10, tessellation: 24}, scene);
  else if (shape === 'cone') template = B.MeshBuilder.CreateCylinder(name, {height: 1, diameterTop: .025, diameterBottom: 1, tessellation: 16}, scene);
  else template = B.MeshBuilder.CreateSphere(name, {diameter: 1, segments: 16}, scene);
  template.isPickable = false;
  template.setEnabled(false);
  resources.set(shape, template);
  return template;
}

function buildContext(spec, context, materials, meshes) {
  const m = context.metrics || {}, c = {
    B: window.BABYLON, scene: context.scene, id: context.id, joints: context.joints,
    tw: vector(m.torsoWidth, 7), th: vector(m.torsoHeight, 8.15), td: vector(m.torsoDepth, 3.75),
    hw: vector(m.headWidth, 4.35), hh: vector(m.headHeight, 3.55), hd: vector(m.headDepth, 3.65),
    ua: vector(m.upperArmLength, 4.25), fa: vector(m.forearmLength, 3.65), aw: vector(m.armWidth, 1.28),
    ul: vector(m.upperLegLength, 5.25), sl: vector(m.shinLength, 4.85), lw: vector(m.legWidth, 1.72),
    bodyY: vector(m.bodyY, 10.56), headY: vector(m.headY, 11.26), shoulderY: vector(m.shoulderY, 7.47),
  };
  c.add = (shape, name, parent, role, size, pos = [0,0,0], rot) => {
    const mesh = nearPrimitiveTemplate(c.B, shape, c.scene).clone(`body-form-${spec.key}-${name}-${c.id}`);
    if (!mesh) throw new Error(`Unable to clone ${shape}`);
    mesh.parent = typeof parent === 'string' ? c.joints[parent] : parent;
    mesh.material = materials[role]; mesh.isPickable = false; mesh.setEnabled(true);
    mesh.scaling.set(...size); mesh.position.set(...pos);
    if (rot) mesh.rotation.set(...rot);
    meshes.push(mesh); return mesh;
  };
  return c;
}

function segment(c, name, joint, role, from, to, width, shape = 'limb') {
  const dx = to[0]-from[0], dy = to[1]-from[1], dz = to[2]-from[2];
  const length = Math.max(.01, Math.hypot(dx,dy,dz));
  return c.add(shape, name, joint, role, [width,length,width],
    [(from[0]+to[0])/2,(from[1]+to[1])/2,(from[2]+to[2])/2],
    [Math.acos(Math.max(-1,Math.min(1,dy/length))),Math.atan2(dx,dz),0]);
}

function legs(c, {thick = 1, foot = 'boot', upper = 'primary', lower = 'primary', sole = 'secondary', bone = false, spread = 1} = {}) {
  for (const label of ['left','right']) {
    c.add(bone ? 'bone' : 'limb', `${label}-thigh`, `${label}Leg`, upper,
      [c.lw*thick,c.ul*1.03,c.lw*thick*1.16], [0,-c.ul*.49,0]);
    c.add(bone ? 'bone' : 'limb', `${label}-shin`, `${label}Knee`, lower,
      [c.lw*thick*.8,c.sl*1.02,c.lw*thick*.9], [0,-c.sl*.48,0]);
    const h = foot === 'sphere' ? .76 : .65;
    c.add(foot, `${label}-foot`, c.joints[`${label}Foot`] || c.joints[`${label}Knee`], sole,
      [c.lw*thick*1.45*spread,h,c.lw*thick*2.3],
      [0,c.joints[`${label}Foot`] ? h*.45 : -c.sl+h*.45,-c.lw*thick*.44]);
  }
}
function arms(c, {thick = 1, upper = 'primary', lower = 'primary', bone = false} = {}) {
  for (const label of ['left','right']) {
    c.add(bone ? 'bone' : 'limb', `${label}-upper-arm`, `${label}Arm`, upper,
      [c.aw*thick,c.ua*1.02,c.aw*thick*1.06], [0,-c.ua*.49,0]);
    c.add(bone ? 'bone' : 'limb', `${label}-forearm`, `${label}Elbow`, lower,
      [c.aw*thick*.86,c.fa*1.08,c.aw*thick*.94], [0,-c.fa*.49,0]);
  }
}
function face(c, {size = [1,1,1], pos = [0,0,0], nose = true, role = 'accent'} = {}) {
  c.add(nose ? 'face' : 'eyes', 'face-details', 'head', role,
    [c.hw*size[0],c.hh*size[1],c.hd*size[2]],pos);
}
function ears(c, {long = false, size = [.33,.68,.32], pos = [.36,.60,0], role = 'primary'} = {}) {
  for (const side of [-1,1]) c.add(long ? 'longEar' : 'ear', `ear-${side}`, 'head', role,
    [c.hw*size[0],c.hh*size[1],c.hd*size[2]],
    [side*c.hw*pos[0],c.hh*pos[1],c.hd*pos[2]], [0,0,-side*.13]);
}
function anchors(c, body, head, top = .62, back = [0,4.7,c.td*.66]) {
  return {body,head,anchors:{headTopY:c.hh*top,backPos:back}};
}

function buildChicken(c) {
  const body=c.add('pear','body','body','primary',[c.tw*1.23,c.th*1.3,c.td*1.62],[0,3.8,.2]);
  c.add('chest','breast','body','primary',[c.tw*.83,c.th*.78,c.td*.56],[0,4.2,-c.td*.61]);
  c.add('limb','neck','head','primary',[c.hw*.53,c.hh*1.18,c.hd*.53],[0,-c.hh*.65,0]);
  const head=c.add('sphere','head','head','primary',[c.hw,c.hh*1.1,c.hd],[0,0,0]);
  c.add('fin','beak','head','secondary',[c.hw*.41,c.hd*.66,c.hh*.31],[0,-c.hh*.14,-c.hd*.58],[-Math.PI/2,0,0]);
  face(c,{size:[1,1.1,1],nose:false});
  c.add('longEar','wattle','head','secondary',[c.hw*.2,c.hh*.5,c.hd*.22],[0,-c.hh*.5,-c.hd*.42]);
  for(const [i,z] of [-.23,0,.23].entries()) c.add('longEar',`comb-${i}`,'head','secondary',
    [c.hw*.16,c.hh*(.5+i*.06),c.hd*.31],[0,c.hh*.58,z*c.hd],[.15,0,0]);
  for(const side of [-1,1]) c.add('fin',`wing-${side}`,side<0?'leftArm':'rightArm','primary',
    [c.tw*.25,c.th*.81,c.td*.62],[side*.15,-c.th*.35,.15],[0,0,-side*.12]);
  for(const side of [-1,0,1]) c.add('fin',`tail-feather-${side}`,'body',side===0?'secondary':'primary',
    [c.tw*.26,c.th*.62,c.td*.19],[side*c.tw*.18,4.5,c.td*.83],[.65,0,-side*.23]);
  legs(c,{thick:.48,foot:'boot',upper:'secondary',lower:'secondary',sole:'secondary',spread:1.25});
  return anchors(c,body,head,1.0);
}
function buildPenguin(c) {
  const body=c.add('pear','body','body','primary',[c.tw*1.15,c.th*1.61,c.td*1.64],[0,3.3,0]);
  c.add('pear','belly','body','secondary',[c.tw*.88,c.th*1.35,c.td*.64],[0,3.0,-c.td*.61]);
  const head=c.add('sphere','head','head','primary',[c.hw,c.hh*1.1,c.hd],[0,-.2,0]);
  for(const side of [-1,1]) c.add('longEar',`cheek-${side}`,'head','secondary',
    [c.hw*.26,c.hh*.62,c.hd*.17],[side*c.hw*.29,-c.hh*.10,-c.hd*.4],[0,0,-side*.18]);
  c.add('fin','beak','head','accent',[c.hw*.32,c.hd*.63,c.hh*.20],[0,-c.hh*.14,-c.hd*.57],[-Math.PI/2,0,0]);
  face(c,{nose:false,role:'primary',pos:[0,-.2,-.06]});
  for(const side of [-1,1]) c.add('fin',`flipper-${side}`,side<0?'leftArm':'rightArm','primary',
    [c.tw*.21,c.th*.94,c.td*.43],[side*.35,-c.th*.4,0],[0,0,-side*.28]);
  legs(c,{thick:.48,sole:'accent',spread:1.7});
  return anchors(c,body,head,.65,[0,4.6,c.td*.85]);
}
function buildCow(c) {
  const body=c.add('barrel','body','body','primary',[c.tw*1.28,c.th*1.04,c.td*1.48],[0,5.0,0]);
  c.add('chest','ruff','body','secondary',[c.tw*.87,c.th*.82,c.td*.55],[0,4.8,-c.td*.56]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.12,c.hh*1.15,c.hd],[0,-.1,0]);
  c.add('sphere','muzzle','head','secondary',[c.hw*.8,c.hh*.48,c.hd*.46],[0,-c.hh*.33,-c.hd*.47]);
  face(c,{size:[1.12,1.15,1],pos:[0,-.1,0]});
  c.add('fin','forelock','head','primary',[c.hw*.86,c.hh*.67,c.hd*.37],[0,c.hh*.27,-c.hd*.38],[0,0,.12]);
  ears(c,{size:[.44,.26,.22],pos:[.54,-.01,0],role:'secondary'});
  for(const side of [-1,1]) c.add('horn',`horn-${side}`,'head','secondary',
    [c.hw*.3,c.hh*1.02,c.hd*.3],[side*c.hw*.66,c.hh*.44,0],[0,0,-side*.76]);
  c.add('tail','tail','body','primary',[c.tw*.14,c.th*.6,c.td*.22],[0,.7,c.td*.83],[.3,0,0]);
  legs(c,{thick:.95,sole:'accent'}); arms(c,{thick:.9});
  return anchors(c,body,head,1.1);
}
function buildCorgi(c) {
  const body=c.add('pear','body','body','primary',[c.tw*1.03,c.th*1.15,c.td*1.55],[0,4.9,.3]);
  c.add('chest','bib','body','secondary',[c.tw*.74,c.th*.86,c.td*.5],[0,4.7,-c.td*.57]);
  const head=c.add('sphere','head','head','primary',[c.hw*1.19,c.hh*1.1,c.hd*1.08],[0,-.15,0]);
  c.add('jaw','muzzle','head','secondary',[c.hw*.62,c.hh*.41,c.hd*.51],[0,-c.hh*.29,-c.hd*.47]);
  face(c,{size:[1.19,1.1,1.08],pos:[0,-.15,0]});
  ears(c,{size:[.43,.92,.32],pos:[.40,.65,.02]});
  c.add('tail','tail','body','secondary',[c.tw*.28,c.th*.35,c.td*.38],[0,2.2,c.td*.83],[.65,0,0]);
  legs(c,{thick:.83,foot:'sphere',lower:'secondary'}); arms(c,{thick:.83,lower:'secondary'});
  return anchors(c,body,head,1.15);
}
function buildCat(c) {
  const body=c.add('torso','body','body','primary',[c.tw*.91,c.th*1.06,c.td*1.14],[0,5.3,0]);
  c.add('chest','bib','body','secondary',[c.tw*.58,c.th*.72,c.td*.31],[0,5.5,-c.td*.5]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.08,c.hh,c.hd],[0,0,0]);
  c.add('sphere','muzzle','head','secondary',[c.hw*.59,c.hh*.36,c.hd*.36],[0,-c.hh*.28,-c.hd*.49]);
  face(c,{size:[1.08,1,1]}); ears(c,{size:[.36,.57,.32],pos:[.36,.61,0]});
  segment(c,'tail-base','body','primary',[0,1.0,c.td*.52],[1.1,4.3,c.td*1.32],c.tw*.14,'tail');
  segment(c,'tail-tip','body','accent',[1.1,4.3,c.td*1.32],[.5,6.5,c.td*1.18],c.tw*.13,'tail');
  for(const side of [-1,1]) c.add('fin',`temple-stripe-${side}`,'head','accent',
    [c.hw*.20,c.hh*.48,c.hd*.08],[side*c.hw*.39,c.hh*.01,-c.hd*.33],[0,0,-side*.65]);
  legs(c,{thick:.68,foot:'sphere'}); arms(c,{thick:.66,lower:'secondary'});
  return anchors(c,body,head,.95);
}
function buildFox(c) {
  const body=c.add('torso','body','body','primary',[c.tw*.94,c.th*1.08,c.td*1.2],[0,5.2,0]);
  c.add('chest','ruff','body','secondary',[c.tw*.8,c.th*.81,c.td*.58],[0,5.1,-c.td*.48]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.08,c.hh*.99,c.hd*1.03],[0,0,0]);
  c.add('fin','muzzle','head','secondary',[c.hw*.62,c.hd*.73,c.hh*.40],[0,-c.hh*.20,-c.hd*.46],[-Math.PI/2,0,0]);
  face(c,{size:[1.08,.99,1.03]}); ears(c,{size:[.37,.8,.28],pos:[.38,.67,.04]});
  c.add('tail','brush','body','primary',[c.tw*.58,c.th*.91,c.td*.77],[.3,2.5,c.td*1.09],[.94,0,-.2]);
  c.add('tail','brush-tip','body','secondary',[c.tw*.36,c.th*.44,c.td*.51],[.9,4.0,c.td*1.88],[.72,0,-.2]);
  legs(c,{thick:.68,foot:'sphere',lower:'accent',sole:'accent'}); arms(c,{thick:.65,lower:'accent'});
  return anchors(c,body,head,1.09);
}
function buildRabbit(c) {
  const body=c.add('pear','body','body','primary',[c.tw*.95,c.th*1.12,c.td*1.43],[0,4.9,.1]);
  c.add('chest','belly','body','secondary',[c.tw*.69,c.th*.77,c.td*.45],[0,4.2,-c.td*.57]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.03,c.hh*1.11,c.hd],[0,0,0]);
  c.add('sphere','muzzle','head','secondary',[c.hw*.58,c.hh*.39,c.hd*.42],[0,-c.hh*.28,-c.hd*.45]);
  face(c,{size:[1.03,1.11,1]}); ears(c,{long:true,size:[.28,1.46,.30],pos:[.28,1.04,0]});
  c.add('sphere','tail','body','secondary',[c.tw*.3,c.tw*.3,c.tw*.3],[0,1.0,c.td*.75]);
  legs(c,{thick:.84,foot:'sphere',sole:'secondary',spread:1.3}); arms(c,{thick:.7,lower:'secondary'});
  return anchors(c,body,head,1.84);
}
function buildFrog(c) {
  const body=c.add('pear','body','body','primary',[c.tw*1.16,c.th*1.06,c.td*1.49],[0,5.1,.2]);
  c.add('sphere','throat','body','secondary',[c.tw*.86,c.th*.61,c.td*.7],[0,7.8,-c.td*.48]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.40,c.hh*.77,c.hd*1.16],[0,-.2,0]);
  c.add('jaw','lower-jaw','head','secondary',[c.hw*1.24,c.hh*.2,c.hd*1.04],[0,-c.hh*.31,-.22]);
  for(const side of [-1,1]) c.add('sphere',`eye-lobe-${side}`,'head','secondary',
    [c.hw*.36,c.hh*.51,c.hd*.34],[side*c.hw*.43,c.hh*.35,-c.hd*.17]);
  face(c,{nose:false,size:[1.83,1.25,.70],pos:[0,c.hh*.23,-c.hd*.1]});
  legs(c,{thick:1.13,foot:'sphere',lower:'secondary',spread:1.22}); arms(c,{thick:.75,lower:'secondary'});
  return anchors(c,body,head,.69);
}
function buildShark(c) {
  const body=c.add('torso','body','body','primary',[c.tw*1.07,c.th*1.22,c.td*1.4],[0,4.5,.25]);
  c.add('chest','belly','body','secondary',[c.tw*.82,c.th*.97,c.td*.5],[0,4.6,-c.td*.59]);
  const head=c.add('jaw','snout','head','primary',[c.hw*1.24,c.hh*.94,c.hd*1.62],[0,-.13,-c.hd*.13]);
  c.add('jaw','jaw','head','secondary',[c.hw*1.06,c.hh*.33,c.hd*1.36],[0,-c.hh*.37,-c.hd*.27]);
  face(c,{nose:false,size:[1.65,.94,1.44],pos:[0,.12,-c.hd*.13]});
  c.add('fin','dorsal','body','primary',[c.tw*.19,c.th*.69,c.td*.92],[0,7.4,c.td*.64],[.12,0,0]);
  segment(c,'tail','body','primary',[0,1.8,c.td*.44],[0,2.6,c.td*1.75],c.tw*.37,'tail');
  c.add('fin','caudal-upper','body','primary',[c.tw*.17,c.th*.62,c.td*.53],[0,4.2,c.td*1.80],[.5,0,0]);
  c.add('fin','caudal-lower','body','secondary',[c.tw*.15,c.th*.40,c.td*.42],[0,1.3,c.td*1.84],[2.45,0,0]);
  c.add('fin','tooth','head','secondary',[c.hw*.22,c.hh*.34,c.hd*.1],[0,-c.hh*.35,-c.hd*.91],[Math.PI,0,0]);
  legs(c,{thick:.81,foot:'sphere'}); arms(c,{thick:.83,lower:'secondary'});
  return anchors(c,body,head,.63);
}
function buildRex(c) {
  const body=c.add('pear','body','body','primary',[c.tw*1.18,c.th*1.21,c.td*1.67],[0,4.4,.5]);
  c.add('chest','belly','body','secondary',[c.tw*.72,c.th*.88,c.td*.56],[0,4.5,-c.td*.64]);
  const head=c.add('jaw','head','head','primary',[c.hw*1.30,c.hh*1.15,c.hd*1.69],[0,-.1,-c.hd*.15]);
  c.add('jaw','lower-jaw','head','secondary',[c.hw*1.14,c.hh*.33,c.hd*1.51],[0,-c.hh*.46,-c.hd*.19]);
  face(c,{nose:false,size:[1.72,1.15,1.43],pos:[0,.16,-c.hd*.15]});
  for(const side of [-1,1]) c.add('longEar',`brow-${side}`,'head','primary',
    [c.hw*.25,c.hh*.6,c.hd*.3],[side*c.hw*.42,c.hh*.22,-c.hd*.55],[Math.PI/2,0,side*.15]);
  segment(c,'tail-base','body','primary',[0,1.0,c.td*.6],[0,1.8,c.td*1.92],c.tw*.51,'tail');
  segment(c,'tail-tip','body','primary',[0,1.8,c.td*1.8],[0,2.9,c.td*2.75],c.tw*.30,'tail');
  for(const side of [-1,1]) c.add('cone',`tooth-${side}`,'head','secondary',
    [c.hw*.14,c.hh*.28,c.hd*.13],[side*c.hw*.29,-c.hh*.42,-c.hd*.92],[Math.PI,0,0]);
  legs(c,{thick:1.22,foot:'boot',sole:'accent'}); arms(c,{thick:.55,lower:'secondary'});
  return anchors(c,body,head,.75,[0,5,c.td*.75]);
}
function buildAdventurer(c) {
  const body=c.add('torso','jacket','body','secondary',[c.tw,c.th*.98,c.td*1.08],[0,5.05,0]);
  c.add('barrel','belt','body','accent',[c.tw*.77,c.th*.12,c.td*.9],[0,1.9,-.01]);
  c.add('limb','neck','head','primary',[c.hw*.36,c.hh*.57,c.hd*.4],[0,-c.hh*.63,0]);
  const head=c.add('jaw','head','head','primary',[c.hw*.91,c.hh*1.12,c.hd*.91],[0,0,0]);
  c.add('jaw','hair','head','accent',[c.hw*.96,c.hh*.53,c.hd*.95],[0,c.hh*.38,.1],[.04,0,.08]);
  face(c,{nose:false,size:[.91,1.12,.91]});
  c.add('limb','nose','head','primary',[c.hw*.13,c.hh*.22,c.hd*.20],[0,-c.hh*.10,-c.hd*.46],[.2,0,0]);
  c.add('chest','pack','body','accent',[c.tw*.58,c.th*.59,c.td*.43],[0,5.5,c.td*.61]);
  legs(c,{thick:.84,upper:'secondary',lower:'secondary',sole:'accent'}); arms(c,{thick:.9,upper:'secondary',lower:'primary'});
  return anchors(c,body,head,.76);
}
function buildAstronaut(c) {
  const body=c.add('torso','pressure-suit','body','primary',[c.tw*1.06,c.th,c.td*1.25],[0,5.0,0]);
  c.add('chest','chest-control','body','secondary',[c.tw*.57,c.th*.39,c.td*.2],[0,5.9,-c.td*.60]);
  c.add('torus','collar','head','secondary',[c.hw*1.14,c.hh*.65,c.hd*1.14],[0,-c.hh*.47,0]);
  const head=c.add('sphere','helmet','head','primary',[c.hw*1.25,c.hh*1.40,c.hd*1.3],[0,0,0]);
  c.add('sphere','visor','head','accent',[c.hw*1.05,c.hh*.97,c.hd*.51],[0,.04,-c.hd*.48]);
  c.add('chest','life-support','body','secondary',[c.tw*.69,c.th*.78,c.td*.66],[0,5.0,c.td*.65]);
  for(const side of [-1,1]) c.add('limb',`shoulder-ring-${side}`,side<0?'leftArm':'rightArm','secondary',
    [c.aw*1.46,c.ua*.28,c.aw*1.47],[0,-c.ua*.12,0]);
  c.add('longEar','visor-reflection','head','secondary',[c.hw*.07,c.hh*.59,c.hd*.04],[-c.hw*.31,c.hh*.12,-c.hd*.72],[0,0,-.25]);
  legs(c,{thick:1.02,sole:'secondary'}); arms(c,{thick:1.10,lower:'primary'});
  return anchors(c,body,head,.82);
}
function buildKnight(c) {
  const body=c.add('torso','cuirass','body','primary',[c.tw*1.09,c.th,c.td*1.12],[0,5.0,0]);
  c.add('chest','breast-ridge','body','primary',[c.tw*.75,c.th*.61,c.td*.40],[0,5.8,-c.td*.5]);
  c.add('robe','fauld','body','secondary',[c.tw*.98,c.th*.38,c.td*1.07],[0,1.0,0]);
  const head=c.add('jaw','closed-helm','head','primary',[c.hw*1.05,c.hh*1.25,c.hd*1.03],[0,0,0]);
  c.add('jaw','visor','head','secondary',[c.hw*.91,c.hh*.16,c.hd*.26],[0,c.hh*.12,-c.hd*.46]);
  c.add('fin','crest','head','accent',[c.hw*.16,c.hh*.95,c.hd*.84],[0,c.hh*.63,.3],[.12,0,0]);
  for(const side of [-1,1]) c.add('chest',`pauldron-${side}`,side<0?'leftArm':'rightArm','primary',
    [c.tw*.36,c.th*.33,c.td*.81],[side*.15,-.55,0],[0,0,-side*.16]);
  c.add('torus','gorget','head','accent',[c.hw*.94,c.hh*.51,c.hd*.94],[0,-c.hh*.51,0]);
  c.add('jaw','helmet-keel','head','primary',[c.hw*.13,c.hh*.75,c.hd*.32],[0,-c.hh*.16,-c.hd*.49]);
  legs(c,{thick:.95,lower:'secondary',sole:'primary'}); arms(c,{thick:.9,upper:'secondary',lower:'primary'});
  return anchors(c,body,head,1.14);
}
function buildWizard(c) {
  const body=c.add('robe','coat','body','primary',[c.tw*1.33,c.th*1.68,c.td*1.74],[0,2.8,0]);
  c.add('chest','stole','body','secondary',[c.tw*.54,c.th*.77,c.td*.19],[0,5.5,-c.td*.47]);
  const head=c.add('jaw','head','head','secondary',[c.hw*.95,c.hh*1.1,c.hd*.94],[0,0,0]);
  face(c,{nose:false,size:[.95,1.1,.94],role:'primary'});
  c.add('tail','beard','head','secondary',[c.hw*.68,c.hh*1.22,c.hd*.37],[0,-c.hh*.7,-c.hd*.44],[Math.PI,0,.07]);
  c.add('hat','hat-crown','head','primary',[c.hw*1.04,c.hh*1.95,c.hd*1.03],[0,c.hh*1.28,0]);
  c.add('sphere','hat-brim','head','primary',[c.hw*1.65,c.hh*.18,c.hd*1.42],[0,c.hh*.39,0],[0,0,.04]);
  c.add('torus','rune','body','accent',[c.tw*.26,c.tw*.26,c.td*.23],[0,6.4,-c.td*.61],[Math.PI/2,0,0]);
  c.add('barrel','belt','body','primary',[c.tw*.77,c.th*.12,c.td*.99],[0,2.7,0]);
  legs(c,{thick:.72,sole:'secondary'}); arms(c,{thick:1.08,lower:'secondary'});
  return anchors(c,body,head,2.30);
}
function buildSkeleton(c) {
  const body=c.add('bone','spine','body','primary',[c.tw*.14,c.th*1.11,c.td*.17],[0,5.0,c.td*.2]);
  c.add('jaw','pelvis','body','primary',[c.tw*.72,c.th*.26,c.td*.63],[0,1.1,0]);
  for(let i=0;i<3;i+=1) c.add('torus',`rib-${i}`,'body','primary',
    [c.tw*(.86-i*.12),c.th*.37,c.td*.99],[0,7.0-i*1.45,0]);
  const head=c.add('jaw','skull','head','primary',[c.hw*1.02,c.hh*1.05,c.hd],[0,.1,0]);
  c.add('skullFace','sockets','head','secondary',[c.hw*1.02,c.hh*1.05,c.hd],[0,.1,0]);
  c.add('jaw','mandible','head','primary',[c.hw*.67,c.hh*.28,c.hd*.54],[0,-c.hh*.46,-c.hd*.12]);
  c.add('eyes','socket-glow','head','accent',[c.hw*1.02,c.hh*1.05,c.hd],[0,.1,-c.hd*.09]);
  legs(c,{thick:.55,upper:'primary',lower:'primary',sole:'primary',bone:true}); arms(c,{thick:.47,bone:true});
  return anchors(c,body,head,.71);
}
function buildGolem(c) {
  const body=c.add('rock','body','body','primary',[c.tw*1.37,c.th*1.10,c.td*1.44],[0,4.9,0]);
  c.add('rock','pelvis','body','secondary',[c.tw*.95,c.th*.34,c.td*1.06],[0,.8,0]);
  c.add('torus','heart-rune','body','accent',[c.tw*.31,c.tw*.31,c.td*.16],[0,6.2,-c.td*.73],[Math.PI/2,0,0]);
  const head=c.add('rock','head','head','primary',[c.hw*1.10,c.hh*.94,c.hd*1.02],[0,-c.hh*.19,0]);
  face(c,{nose:false,size:[1.10,.94,1.02],pos:[0,-c.hh*.19,0]});
  c.add('rock','brow','head','secondary',[c.hw*.97,c.hh*.22,c.hd*.35],[0,.11,-c.hd*.38],[0,0,.05]);
  for(const side of [-1,1]) {
    c.add('rock',`shoulder-${side}`,side<0?'leftArm':'rightArm','primary',
      [c.tw*.44,c.th*.39,c.td*.94],[side*.25,-.8,0],[0,side*.13,side*.17]);
    c.add('rock',`fist-${side}`,side<0?'leftElbow':'rightElbow','secondary',
      [c.aw*1.9,c.fa*.55,c.aw*2.01],[0,-c.fa*.8,-.1],[0,side*.12,0]);
  }
  legs(c,{thick:1.17,foot:'rock',lower:'secondary',sole:'secondary'}); arms(c,{thick:1.12,upper:'secondary',lower:'secondary'});
  return anchors(c,body,head,.42);
}
function buildSlime(c) {
  const floor=-c.bodyY;
  const body=c.add('pear','gel-body','body','primary',[c.tw*1.41,c.th*1.20,c.td*1.98],[0,floor+c.th*.59,0]);
  c.add('sphere','foot-pool','body','primary',[c.tw*1.58,c.th*.16,c.td*2.15],[0,floor+c.th*.08,0]);
  for(const side of [-1,1]) c.add('sphere',`lobe-${side}`,'body','primary',
    [c.tw*.42,c.th*.33,c.td*.73],[side*c.tw*.59,floor+c.th*.17,-c.td*.2]);
  const faceY=floor+c.th*.82-c.headY;
  const head=c.add('sphere','face','head','primary',[c.tw*.83,c.th*.55,c.td*1.01],[0,faceY,-c.td*.51]);
  c.add('eyes','eyes','head','secondary',[c.tw*.83,c.th*.55,c.td*1.01],[0,faceY,-c.td*.51]);
  c.add('sphere','smile','head','secondary',[c.tw*.18,c.th*.045,c.td*.04],[0,faceY-c.th*.11,-c.td*1.015]);
  c.add('torus','diadem','head','accent',[c.tw*.60,c.th*.31,c.td*.91],[0,faceY+c.th*.38,0]);
  for(const side of [-1,0,1]) c.add('horn',`crown-point-${side}`,'head','accent',
    [c.tw*.15,c.th*(side===0?.42:.29),c.td*.19],[side*c.tw*.23,faceY+c.th*.51,0],[0,0,-side*.25]);
  c.add('sphere','gel-highlight','body','secondary',[c.tw*.20,c.th*.19,c.td*.025],[-c.tw*.31,floor+c.th*.80,-c.td*.75]);
  if(c.joints.core?.position) c.joints.core.position.set(0,floor+c.th*.61,-c.td*.96);
  return {body,head,anchors:{headTopY:faceY+c.th*.81,shoulderY:floor+c.th*.68-c.shoulderY,backPos:[0,floor+c.th*.75,c.td*.79]}};
}
function buildDrone(c) {
  const stand=-c.bodyY*.14, floor=-c.bodyY+.08;
  const body=c.add('chest','carapace','body','primary',[c.tw*1.29,c.th*.51,c.td*1.59],[0,stand,0],[.08,0,0]);
  c.add('pear','abdomen','body','secondary',[c.tw*.81,c.th*.5,c.td*1.16],[0,stand+.2,c.td*.96],[.3,0,0]);
  const head=c.add('jaw','sensor-head','head','primary',[c.tw*.60,c.th*.29,c.td*.9],[0,stand-c.headY,-c.td*.86]);
  c.add('sphere','optic','head','accent',[c.tw*.23,c.tw*.23,c.td*.19],[0,stand-c.headY+.12,-c.td*1.34]);
  for(const side of [-1,1]) {
    c.add('horn',`mandible-${side}`,'body','secondary',[c.tw*.18,c.th*.40,c.td*.26],
      [side*c.tw*.27,stand-.5,-c.td*1.12],[Math.PI/2,0,-side*.25]);
    for(let i=0;i<4;i+=1) {
      const z=i-1.5;
      const hip=[side*c.tw*.48,stand+.45,z*c.td*.43];
      const knee=[side*c.tw*(1.04+(.5-Math.abs(z))*.06),stand+2.2,z*c.td*.91];
      const tip=[side*c.tw*(1.47+(.5-Math.abs(z))*.09),floor,z*c.td*1.36];
      segment(c,`femur-${side}-${i}`,'body','primary',hip,knee,c.tw*.12,'limb');
      segment(c,`tibia-${side}-${i}`,'body','secondary',knee,tip,c.tw*.105,'horn');
    }
  }
  if(c.joints.core?.position) c.joints.core.position.set(0,stand+c.th*.22,-c.td*.28);
  return {body,head,anchors:{headTopY:stand-c.headY+c.th*.21,shoulderY:stand-c.shoulderY,backPos:[0,stand+1.6,c.td*.74]}};
}

const FORM_BUILDERS=Object.freeze({
  giant_chicken:buildChicken,emperor_penguin:buildPenguin,highland_cow:buildCow,
  corgi:buildCorgi,tabby_cat:buildCat,red_fox:buildFox,battle_rabbit:buildRabbit,
  bullfrog:buildFrog,land_shark:buildShark,tyrant_rex:buildRex,human_adventurer:buildAdventurer,
  astronaut:buildAstronaut,knight:buildKnight,wizard:buildWizard,skeleton:buildSkeleton,
  stone_golem:buildGolem,slime_monarch:buildSlime,spider_drone:buildDrone,
});

/** Build one bounded, articulated form on the shared Forge joints. */
export function buildBodyFormGeometry(spec, context) {
  if(!spec || !context?.scene || !context?.joints) throw new TypeError('A body form, scene, and Forge joints are required');
  const id=String(context.id || spec.key).slice(0,96), meshes=[];
  const materials=makeMaterials(spec,context.scene,id);
  try {
    const c=buildContext(spec,{...context,id},materials,meshes);
    const canonical=(FORM_BUILDERS[spec.key] || buildAdventurer)(c);
    if(meshes.length>spec.nearMeshBudget) throw new Error(`${spec.key} exceeded its ${spec.nearMeshBudget}-mesh budget`);
    return {meshes,materials:materials.all,body:canonical.body,head:canonical.head,anchors:canonical.anchors || null};
  } catch(error) {
    for(const mesh of meshes) mesh.dispose();
    for(const material of materials.all) material.dispose();
    throw error;
  }
}

function farSignatureParts(spec) {
  const parts=[];
  const add=(shape,position,scaling,rotation)=>parts.push({shape,position,scaling,rotation});
  const span=(from,to,width)=>{
    const dx=to[0]-from[0],dy=to[1]-from[1],dz=to[2]-from[2],length=Math.hypot(dx,dy,dz);
    add('limb',[(from[0]+to[0])/2,(from[1]+to[1])/2,(from[2]+to[2])/2],
      [width,length,width],[Math.acos(dy/length),Math.atan2(dx,dz),0]);
  };
  if(spec.family==='slime') {
    add('pear',[0,.21,0],[.97,.43,.97]);
    add('sphere',[0,.035,0],[1.05,.07,1.03]);
    for(const x of [-.18,0,.18]) add('horn',[x,.48,0],[.12,x===0?.17:.13,.12]);
    return parts;
  }
  if(spec.family==='drone') {
    add('chest',[0,.35,0],[.79,.18,.87]);
    add('pear',[0,.36,.47],[.58,.17,.71]);
    add('jaw',[0,.34,-.53],[.40,.1,.43]);
    for(const side of [-1,1]) for(let i=0;i<4;i+=1) {
      const z=(i-1.5)*.27;
      span([side*.32,.36,z],[side*.73,.47,z*1.9],.055);
      span([side*.73,.47,z*1.9],[side*1.04,.015,z*2.8],.042);
    }
    return parts;
  }
  const isBird=spec.family==='avian', isAnimal=['mammal','amphibian','marine','reptile'].includes(spec.family);
  add(spec.key==='wizard'?'robe':spec.key==='stone_golem'?'rock':isBird?'pear':'torso',
    [0,.58,0],[spec.key==='stone_golem'?.85:.66,spec.key==='wizard'?.60:.40,isAnimal?.79:.65]);
  add(spec.key==='astronaut'?'sphere':'jaw',[0,.87,-.015],
    [spec.key==='tyrant_rex'?.59:.44,spec.key==='astronaut'?.21:.17,spec.key==='tyrant_rex'?.98:.61]);
  for(const side of [-1,1]) {
    add('limb',[side*.19,.28,0],[.14,.33,.18]);
    add('boot',[side*.19,.075,-.08],[.21,.075,.36]);
    add(isBird?'fin':'limb',[side*.40,.54,0],[isBird?.14:.12,.31,isBird?.27:.17],[0,0,-side*.08]);
  }
  const signature={
    giant_chicken:()=>{
      add('fin',[0,.87,-.46],[.17,.23,.13],[-Math.PI/2,0,0]);
      for(const z of [-.13,0,.13]) add('longEar',[0,1.0,z],[.065,.13,.14]);
      for(const side of [-1,0,1]) add('fin',[side*.13,.68,.43],[.17,.29,.11],[.5,0,-side*.22]);
    },
    emperor_penguin:()=>add('fin',[0,.86,-.43],[.13,.19,.10],[-Math.PI/2,0,0]),
    highland_cow:()=>{
      add('jaw',[0,.82,-.32],[.34,.09,.24]);
      for(const side of [-1,1]) add('horn',[side*.31,1.0,0],[.12,.22,.12],[0,0,-side*.7]);
    },
    corgi:()=>{
      for(const side of [-1,1]) add('ear',[side*.17,1.02,0],[.18,.24,.16],[0,0,-side*.13]);
      add('jaw',[0,.83,-.32],[.29,.07,.28]);
    },
    tabby_cat:()=>{
      for(const side of [-1,1]) add('ear',[side*.16,1.0,0],[.14,.13,.13]);
      span([0,.48,.34],[.15,.69,.73],.09);span([.15,.69,.73],[.1,.8,.65],.08);
    },
    red_fox:()=>{
      for(const side of [-1,1]) add('ear',[side*.17,1.01,0],[.15,.21,.13]);
      add('tail',[0,.47,.67],[.40,.39,.56],[1.0,0,-.18]);
      add('fin',[0,.83,-.30],[.25,.23,.16],[-Math.PI/2,0,0]);
    },
    battle_rabbit:()=>{for(const side of [-1,1]) add('longEar',[side*.12,1.11,0],[.13,.38,.15],[0,0,-side*.13]);},
    bullfrog:()=>{for(const side of [-1,1]) add('sphere',[side*.2,.98,-.09],[.18,.10,.19]);},
    land_shark:()=>{
      add('fin',[0,.75,.33],[.12,.31,.47]);add('tail',[0,.47,.71],[.25,.39,.35],[1.0,0,0]);
      add('fin',[0,.59,.89],[.08,.26,.24],[.3,0,0]);
    },
    tyrant_rex:()=>add('tail',[0,.48,.89],[.35,.67,.40],[1.12,0,0]),
    human_adventurer:()=>add('chest',[0,.61,.40],[.40,.25,.23]),
    astronaut:()=>add('chest',[0,.58,.40],[.45,.35,.32]),
    knight:()=>{
      add('fin',[0,1.02,.02],[.06,.22,.29]);
      for(const side of [-1,1]) add('chest',[side*.4,.72,0],[.25,.14,.31]);
    },
    wizard:()=>{
      add('hat',[0,1.07,0],[.44,.37,.49]);add('sphere',[0,.96,0],[.72,.035,.71]);
    },
    skeleton:()=>{for(const y of [.58,.64,.70]) add('torus',[0,y,0],[.55,.12,.55]);},
    stone_golem:()=>{for(const side of [-1,1]) add('rock',[side*.44,.70,0],[.34,.19,.4]);},
  };
  signature[spec.key]?.();
  return parts;
}

function farTemplate(spec, scene) {
  let resources = _farResources.get(scene);
  if (!resources) {
    resources = new Map();
    _farResources.set(scene, resources);
  }
  if (resources.has(spec.key)) return resources.get(spec.key);

  const B = window.BABYLON;
  const material = makeMat(`body-form-far-${spec.key}`, scene, parseColor(PALETTES[spec.key]?.[0] || spec.primary), {
    emissiveFactor: 0.18,
  });
  material.backFaceCulling = true;
  material.freeze();
  const pieces = farSignatureParts(spec).map((recipe, index) => {
    const mesh = nearPrimitiveTemplate(B, recipe.shape, scene).clone(`body-form-far-${spec.key}-${index}`);
    if (!mesh) throw new Error(`Unable to clone ${spec.key} far part`);
    mesh.setEnabled(true);
    mesh.position.set(...recipe.position);
    mesh.scaling.set(...recipe.scaling);
    if (recipe.rotation) mesh.rotation.set(...recipe.rotation);
    mesh.material = material;
    mesh.isPickable = false;
    return mesh;
  });
  let template;
  if (typeof B.Mesh?.MergeMeshes === 'function') {
    template = B.Mesh.MergeMeshes(pieces, true, true, undefined, false, true);
  } else {
    template = pieces.shift();
    for (const piece of pieces) piece.dispose();
    template.position.set(0, 0, 0);
    template.scaling.set(1, 1, 1);
  }
  if (!template) throw new Error(`Unable to build ${spec.key} far proxy`);
  template.name = `body-form-far-template-${spec.key}`;
  template.material = material;
  template.isPickable = false;
  template.setEnabled(false);
  resources.set(spec.key, template);
  return template;
}

/** Create one shared-geometry, form-specific far proxy (never the blue generic bot). */
export function createBodyFormFarProxy(spec, scene, parent, dimensions = {}) {
  const template = farTemplate(spec, scene);
  const proxy = template.clone(`body-form-far-${spec.key}`);
  proxy.parent = parent;
  proxy.material = template.material;
  proxy.position.set(0, 0, 0);
  proxy.scaling.set(
    vector(dimensions.width, 10),
    vector(dimensions.height, 24),
    vector(dimensions.depth, 6),
  );
  proxy.isPickable = true;
  proxy.setEnabled(false);
  return proxy;
}
