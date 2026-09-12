'use strict';

/** Structural glass detailing. All geometry stays below the playable y=0 plane. */
export function glassDeckParts(width, depth) {
  if (!Number.isFinite(width) || !Number.isFinite(depth) || width <= 0 || depth <= 0) return [];
  const thickness = Math.min(width, depth) * 0.012;
  const rim = Math.min(width, depth) * 0.006;
  const parts = [];
  const perimeter = (kind, inset, y, height, beam) => {
    parts.push(
      { kind, x: width / 2, y, z: inset, width: width - inset * 2, height, depth: beam },
      { kind, x: width / 2, y, z: depth - inset, width: width - inset * 2, height, depth: beam },
      { kind, x: inset, y, z: depth / 2, width: beam, height, depth: depth - inset * 2 },
      { kind, x: width - inset, y, z: depth / 2, width: beam, height, depth: depth - inset * 2 },
    );
  };
  perimeter('glass', rim / 2, -thickness / 2, thickness, rim);
  perimeter('metal', rim * 2, -thickness - rim, rim * 1.5, rim * 2);
  perimeter('trim', rim / 2, -thickness * 0.82, rim * 0.22, rim * 1.05);
  // Sparse transverse ribs close to the perimeter show the open underside.
  // No opaque bottom plate: the stars remain visible through the central deck.
  for (const t of [0.2, 0.4, 0.6, 0.8]) {
    for (const z of [rim * 6, depth - rim * 6]) {
      parts.push({ kind: 'metal', x: width * t, y: -thickness * 1.5,
        z, width: rim * 1.3, height: rim * 1.6, depth: rim * 10 });
    }
    for (const x of [rim * 6, width - rim * 6]) {
      parts.push({ kind: 'metal', x, y: -thickness * 1.5,
        z: depth * t, width: rim * 10, height: rim * 1.6, depth: rim * 1.3 });
    }
  }
  // Thin load-bearing rails align with the pane seams. The center remains
  // open: these occupy less than two percent of the deck area and join the
  // existing metal batch rather than adding meshes or draw calls.
  for (const t of [0.25, 0.75]) {
    parts.push(
      { kind: 'metal', x: width * t, y: -thickness * 1.15, z: depth / 2,
        width: rim * 0.45, height: thickness * 0.45, depth: depth - rim * 4 },
      { kind: 'metal', x: width / 2, y: -thickness * 1.15, z: depth * t,
        width: width - rim * 4, height: thickness * 0.45, depth: rim * 0.45 },
    );
  }
  return parts;
}

/** Seeded texture grain remains stable when the map palette changes. */
export function deckRandom(seed = 31847) {
  let state = seed >>> 0;
  return () => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return state / 4294967296;
  };
}

/** Fine optical polish, not a large glowing checkerboard. Baked once per scene. */
export function createGlassPolish(scene) {
  const B = window.BABYLON;
  const texture = new B.DynamicTexture('glassPolishTex', 512, scene, false);
  const ctx = texture.getContext();
  const random = deckRandom();
  ctx.fillStyle = 'rgb(172,181,192)';
  ctx.fillRect(0, 0, 512, 512);
  for (let i = 0; i < 1100; i++) {
    const x = random() * 512, y = random() * 512;
    const length = 0.5 + random() * 8;
    ctx.strokeStyle = `rgba(225,238,255,${0.025 + random() * 0.09})`;
    ctx.lineWidth = 0.3;
    ctx.beginPath(); ctx.moveTo(x, y); ctx.lineTo(x + length, y + length * 0.1); ctx.stroke();
  }
  texture.update();
  return texture;
}

export class GlassDeck {
  constructor(scene, width, depth, accent) {
    const B = window.BABYLON;
    this.meshes = [];
    this.materials = [];
    const glass = new B.StandardMaterial('deckEdgeGlassMat', scene);
    glass.diffuseColor = new B.Color3(0.12, 0.25, 0.32);
    glass.emissiveColor = new B.Color3(0.018, 0.052, 0.07);
    glass.specularColor = new B.Color3(0.7, 0.85, 0.95);
    glass.specularPower = 128;
    glass.alpha = 0.52;
    glass.backFaceCulling = true;
    const metal = new B.StandardMaterial('deckFrameMat', scene);
    metal.diffuseColor = new B.Color3(0.19, 0.24, 0.30);
    metal.emissiveColor = new B.Color3(0.015, 0.024, 0.037);
    metal.specularColor = new B.Color3(0.42, 0.5, 0.62);
    metal.specularPower = 72;
    const trim = new B.StandardMaterial('deckEdgeLightMat', scene);
    trim.diffuseColor = B.Color3.Black();
    trim.emissiveColor = new B.Color3(...accent).scale(0.55);
    trim.disableLighting = true;
    this.trim = trim;
    this.materials.push(glass, metal, trim);
    const mats = { glass, metal, trim };
    const batches = { glass: [], metal: [], trim: [] };
    for (const [i, part] of glassDeckParts(width, depth).entries()) {
      const mesh = B.MeshBuilder.CreateBox(`deck-${part.kind}-${i}`, {
        width: part.width, height: part.height, depth: part.depth,
      }, scene);
      mesh.position.set(part.x, part.y, part.z);
      mesh.material = mats[part.kind];
      mesh.isPickable = false;
      mesh.freezeWorldMatrix();
      batches[part.kind].push(mesh);
    }
    // Bake the fixed transforms into three material batches. Keeping a single
    // material per mesh avoids submesh draw calls while retaining open geometry.
    for (const [kind, source] of Object.entries(batches)) {
      const merged = typeof B.Mesh?.MergeMeshes === 'function'
        ? B.Mesh.MergeMeshes(source, true, true, undefined, false, false)
        : null;
      if (merged) {
        merged.name = `deck-${kind}`;
        merged.material = mats[kind];
        merged.isPickable = false;
        merged.freezeWorldMatrix();
        this.meshes.push(merged);
      } else {
        // Small test runtimes may not expose the merger. A failed merge leaves
        // its sources intact, so preserve ownership for rendering and disposal.
        this.meshes.push(...source);
      }
    }
    glass.freeze(); metal.freeze(); trim.freeze();
  }

  setAccent(accent) {
    this.trim.unfreeze();
    this.trim.emissiveColor.set(accent[0] * 0.55, accent[1] * 0.55, accent[2] * 0.55);
    this.trim.freeze();
  }

  dispose() {
    for (const mesh of this.meshes) mesh.dispose();
    for (const material of this.materials) material.dispose();
    this.meshes.length = 0;
    this.materials.length = 0;
  }
}
