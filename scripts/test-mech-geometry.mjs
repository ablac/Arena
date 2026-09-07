import assert from 'node:assert/strict';
import {beveledBox, profileHull} from '../frontend/js/renderer/mech-geometry.js';

// Inspect emitted vertex buffers directly: no graphics driver or visual mocks.
class Mesh { constructor(name) { this.name=name; } }
class VertexData { applyToMesh(mesh) { mesh.data=this; } }
globalThis.window={BABYLON:{Mesh,VertexData}};

function inspect(mesh, expectedBounds) {
  const {positions:p,normals:n,indices,uvs}=mesh.data;
  assert.ok([...p,...n,...uvs].every(Number.isFinite));
  assert.equal(n.length,p.length);
  assert.equal(uvs.length,p.length/3*2);
  for(let axis=0;axis<3;axis++) {
    const values=p.filter((_,i)=>i%3===axis);
    assert.ok(Math.abs(Math.min(...values)-expectedBounds[axis][0])<1e-8);
    assert.ok(Math.abs(Math.max(...values)-expectedBounds[axis][1])<1e-8);
  }
  for(let i=0;i<indices.length;i+=3) {
    const points=indices.slice(i,i+3).map(j=>p.slice(j*3,j*3+3));
    const [a,b,c]=points;
    const u=b.map((v,j)=>v-a[j]),v=c.map((value,j)=>value-a[j]);
    const cross=[u[1]*v[2]-u[2]*v[1],u[2]*v[0]-u[0]*v[2],u[0]*v[1]-u[1]*v[0]];
    assert.ok(Math.hypot(...cross)>1e-10,'no collapsed triangles');
    const normal=n.slice(indices[i]*3,indices[i]*3+3);
    assert.ok(cross.reduce((s,value,j)=>s+value*normal[j],0)<0,'clockwise face matches outward Babylon normal');
    const centroid=a.map((value,j)=>(value+b[j]+c[j])/3);
    assert.ok(normal.reduce((s,value,j)=>s+value*centroid[j],0)>0,'centered solid normals face outward');
  }
}
inspect(beveledBox('armor',{width:4,height:6,depth:2,bevel:0.2},{}),[[-2,2],[-3,3],[-1,1]]);
inspect(profileHull('taper',[
  {y:-3,width:2,depth:2}, {y:0,width:4,depth:3}, {y:3,width:2,depth:2},
],{}),[[-2,2],[-3,3],[-1.5,1.5]]);
assert.throws(()=>profileHull('empty',[],{}),/distinct height/);
assert.throws(()=>profileHull('flat',[{y:0},{y:0}],{}),/distinct height/);
console.log('Armor solids: finite closed face buffers, bounds, outward normals and clockwise winding pass.');
