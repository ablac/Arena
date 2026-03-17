# BabylonJS Performance & Graphics Upgrade

**Date**: 2026-03-17
**Status**: Approved
**Scope**: 5 files, no new files

## Goal

Improve visual quality and rendering performance for spectators watching 50+ bot arena matches. Net FPS should improve (instancing savings exceed pipeline costs).

## Approach: Smart Upgrade

Instancing first to free GPU headroom, then layer on rendering pipeline effects. Keep StandardMaterial everywhere — PBR is unnecessary for this stylized art style.

## 1. Mesh Instancing

### Problem
Each bot creates 6+ unique meshes. At 50 bots = 300+ draw calls.

### Solution
Create hidden template meshes (one per part type). Each bot gets an `InstancedMesh` — same geometry, same material, individual transform. 50 bots ≈ 6 draw calls instead of 300+.

### Template Meshes
| Template | Geometry | Tessellation |
|----------|----------|-------------|
| `tpl-body` | Cylinder | 6 |
| `tpl-head` | Sphere | 4 segments |
| `tpl-arm` | Cylinder | 4 |
| `tpl-shadow` | Disc | 6 |
| `tpl-weapon-{type}` | Per weapon | Varies |

### Per-Bot Color
Use `instancedMesh.instancedBuffers.color` for per-instance vertex color. Replaces per-bot materials with shared materials using vertex color.

### Swordsman Bodies
Each box type in the skeleton hierarchy (torso, upper arm, lower arm, thigh, shin, head) gets its own template. Instances are parented to per-bot TransformNodes for joint articulation.

### Trade-off
Per-bot Fresnel rim lighting is lost (material-level property, shared across instances). Compensated by upgraded glow layer and per-instance emissive color.

### Files
- `bot-body.js` — generic bot instancing
- `swordsman-body.js` — swordsman instancing

## 2. DefaultRenderingPipeline

### Setup (in `engine.js`)
```
Pipeline attached to camera:
- FXAA: enabled (cheaper than MSAA)
- Bloom: threshold 0.3, weight 0.4, kernel 32, scale 0.5
- Sharpen: intensity 0.15
- Tone mapping: ACES filmic
- No DOF, chromatic aberration, or SSAO
```

### Why These Settings
- FXAA removes jagged edges cheaply (single post-process pass)
- Bloom makes emissive weapons, zone rings, and combat effects glow
- Sharpen counteracts FXAA softening
- ACES tone mapping prevents blown highlights, adds richer contrast
- DOF/CA/SSAO excluded: too expensive for 50+ bots, minimal spectator value

### File
- `engine.js`

## 3. Glow Layer Upgrade

### Changes
| Property | Before | After |
|----------|--------|-------|
| `mainTextureFixedSize` | 128 | 256 |
| `intensity` | 0.2 | 0.4 |

Sharper glow halos, more visible bloom on emissive surfaces (weapons, zone rings, combat particles).

### File
- `engine.js`

## 4. Shadow Generator

### Setup (in `environment.js`)
- Single `ShadowGenerator` on existing directional light ("sun")
- Resolution: 1024x1024
- Filter: PCF (Percentage Closer Filtering) for soft edges
- Bias: 0.005, Normal bias: 0.02
- Shadow casters: obstacle meshes only (not bots — too many at 50+)
- Shadow receiver: ground mesh

### Why Obstacles Only
At 50+ bots, per-bot shadow casting would require 50+ meshes in the shadow map per frame. Obstacles are static (frozen world matrix) so the shadow map rarely needs re-rendering. Bots keep their existing flat shadow discs (now instanced).

### Files
- `environment.js` — create shadow generator, register ground as receiver
- `obstacles.js` — register obstacle meshes as shadow casters

## 5. What Stays Unchanged

- StandardMaterial everywhere (no PBR migration)
- Bot animation system (animations.js, swordsman-anims.js)
- Trail ribbon rendering (trails.js)
- Particle effects system (effects.js)
- Camera system (camera.js)
- All existing performance flags (frozen matrices, scene optimizations)
- GUI overlay system (AdvancedDynamicTexture for HP bars/labels)

## Expected Results

| Metric | Before | After |
|--------|--------|-------|
| Draw calls (50 bots) | ~300+ | ~20-30 |
| Anti-aliasing | None | FXAA |
| Bloom | Minimal glow | Cinematic bloom on emissives |
| Shadows | Fake discs only | Real obstacle shadows + instanced discs |
| Glow resolution | 128px | 256px |
| Tone mapping | None | ACES filmic |
| Net FPS impact | Baseline | Improved (instancing savings > pipeline cost) |

## Risk

Fresnel rim lighting loss on bots may make them look flatter. Mitigation: if noticed, add subtle emissive boost to shared instanced material without breaking instancing.
