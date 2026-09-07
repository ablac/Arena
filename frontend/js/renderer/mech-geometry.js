'use strict';

/** Compact, flat-shaded armor solids with real edge planes and stable UVs. */
const positive = (value, fallback = 1) => Number.isFinite(value) && value > 0 ? value : fallback;

function section(ring) {
  const w = positive(ring.width) / 2;
  const d = positive(ring.depth) / 2;
  const b = Math.max(0.00001, Math.min(positive(ring.bevel, Math.min(w, d) * 0.28), w * 0.95, d * 0.95));
  const x = Number.isFinite(ring.x) ? ring.x : 0;
  const z = Number.isFinite(ring.z) ? ring.z : 0;
  return [[-w+b,-d],[w-b,-d],[w,-d+b],[w,d-b],[w-b,d],[-w+b,d],[-w,d-b],[-w,-d+b]]
    .map(([px,pz]) => [px+x, ring.y, pz+z]);
}

/**
 * Loft eight-sided armor sections along local Y. Each ring is
 * {y,width,depth,bevel?,x?,z?}; width/depth are full dimensions.
 * Face vertices are intentionally split for crisp manufactured panel edges.
 */
export function profileHull(name, rings, scene) {
  const B = window.BABYLON;
  const valid = rings.filter(r => Number.isFinite(r?.y)).slice().sort((a,b) => a.y-b.y);
  if (valid.length < 2 || valid[valid.length-1].y <= valid[0].y) {
    throw new Error('Armor hull requires at least two distinct height sections');
  }
  // Tiny renderer hosts can omit custom vertex buffers. Full Babylon always
  // takes the authored solid path; this fallback preserves lifecycle APIs.
  if (typeof B.VertexData !== 'function' || typeof B.Mesh !== 'function') {
    const mesh = B.MeshBuilder.CreateBox(name, {
      width: Math.max(...valid.map(r => positive(r.width))),
      depth: Math.max(...valid.map(r => positive(r.depth))),
      height: valid[valid.length-1].y-valid[0].y,
    }, scene);
    mesh.position.y = (valid[0].y+valid[valid.length-1].y)/2;
    return mesh;
  }
  const positions = [], normals = [], indices = [], uvs = [];
  const face = (points, outward) => {
    const a = points[0], b = points[1], c = points[2];
    const u = b.map((v,i) => v-a[i]), v = c.map((value,i) => value-a[i]);
    let n = [u[1]*v[2]-u[2]*v[1], u[2]*v[0]-u[0]*v[2], u[0]*v[1]-u[1]*v[0]];
    const length = Math.hypot(...n);
    if (length < 1e-12) return;
    n = n.map(value => value/length);
    const reverse = n.reduce((sum,value,i) => sum+value*outward[i],0) < 0;
    if (reverse) n = n.map(value => -value);
    const base = positions.length/3;
    points.forEach((point,i) => {
      positions.push(...point); normals.push(...n);
      // Planar face UVs keep fine metal grain free from cross-edge stretching.
      uvs.push(i === 1 || i === 2 ? 1 : 0, i >= 2 ? 1 : 0);
    });
    for (let i=1; i<points.length-1; i++) {
      // Babylon's left-handed front faces use clockwise index winding.
      indices.push(base, base+(reverse ? i : i+1), base+(reverse ? i+1 : i));
    }
  };
  const sections = valid.map(section);
  for (let j=0; j<sections.length-1; j++) {
    for (let i=0; i<8; i++) {
      const k=(i+1)%8;
      const middle=[(sections[j][i][0]+sections[j][k][0])/2-(valid[j].x||0),0,
        (sections[j][i][2]+sections[j][k][2])/2-(valid[j].z||0)];
      face([sections[j][i],sections[j+1][i],sections[j+1][k],sections[j][k]],middle);
    }
  }
  // Triangle fans on convex octagonal end caps, with independent cap normals.
  for (const [index, direction] of [[0,-1],[sections.length-1,1]]) {
    const ring=sections[index];
    for(let i=1;i<7;i++) face([ring[0],ring[i],ring[i+1]],[0,direction,0]);
  }
  const mesh = new B.Mesh(name,scene);
  const data = new B.VertexData();
  Object.assign(data,{positions,normals,indices,uvs});
  data.applyToMesh(mesh);
  mesh.isPickable=false;
  return mesh;
}

/** Beveled on the vertical corners and across both end faces, centered at 0. */
export function beveledBox(name, {width=1,height=1,depth=1,bevel}={}, scene) {
  width=positive(width); height=positive(height); depth=positive(depth);
  const edge=Math.min(positive(bevel,Math.min(width,height,depth)*0.12),Math.min(width,height,depth)*0.24);
  return profileHull(name,[
    {y:-height/2,width:width-edge*2,depth:depth-edge*2,bevel:edge*0.5},
    {y:-height/2+edge,width,depth,bevel:edge},
    {y:height/2-edge,width,depth,bevel:edge},
    {y:height/2,width:width-edge*2,depth:depth-edge*2,bevel:edge*0.5},
  ],scene);
}
