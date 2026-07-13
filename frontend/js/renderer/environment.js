'use strict';

/**
 * Arena environment — stone floor, boundary walls, dark void, safe zone ring.
 * @module renderer/environment
 */

import { makeMat } from './utils.js';
import { isEnabled } from '../settings.js';

const ZONE_RING_SEGMENTS = 64;
const ZONE_RING_BASE_ALPHA = 0.7;

export class EnvironmentRenderer {
  /** @param {BABYLON.Scene} scene @param {number} w @param {number} h */
  constructor(scene, w, h) {
    this.scene = scene;
    this.w = w;
    this.h = h;
    this._zoneRing = null;
    this._targetRing = null;
    this._zoneMat = null;
    this._targetMat = null;
    this._lastZoneR = -1;
    this._lastZoneCx = -1;
    this._lastZoneCy = -1;
    this._zoneSuddenDeath = false;

    this._createSkybox();
    this._createSpaceObjects();
    this._createFloor();
    this._createWalls();
    this._createCornerPylons();
    this._createPylonRings();
    this._createUndersideThrusters();
    this._createEdgeWaterfalls();
    this._createHoloTitle();
    this._initZoneMaterials();
    this._createAmbientParticles();
  }

  /** @private GPU-procedural space skybox — stars, nebulae, sun. All generated in the fragment shader. */
  _createSkybox() {
    const B = window.BABYLON;

    const skybox = B.MeshBuilder.CreateBox('skybox', { size: 50000 }, this.scene);
    skybox.infiniteDistance = true;
    skybox.renderingGroupId = 0;

    // Procedural space shader — generates stars and nebulae entirely on the GPU
    B.Effect.ShadersStore['spaceVertexShader'] = `
      precision highp float;
      attribute vec3 position;
      uniform mat4 worldViewProjection;
      varying vec3 vDir;
      void main() {
        gl_Position = worldViewProjection * vec4(position, 1.0);
        // Box position IS the direction — normalize for consistent star placement
        vDir = normalize(position);
      }
    `;

    B.Effect.ShadersStore['spaceFragmentShader'] = `
      precision highp float;
      varying vec3 vDir;
      uniform float time;

      // --- Hash functions for pseudo-random stars ---
      float hash(vec3 p) {
        p = fract(p * vec3(443.897, 441.423, 437.195));
        p += dot(p, p.yzx + 19.19);
        return fract((p.x + p.y) * p.z);
      }

      float hash2(vec2 p) {
        return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
      }

      // --- Simplex-ish 3D noise for nebulae ---
      float noise3(vec3 p) {
        vec3 i = floor(p);
        vec3 f = fract(p);
        f = f * f * (3.0 - 2.0 * f);
        float n = dot(i, vec3(1.0, 57.0, 113.0));
        return mix(
          mix(mix(hash(i), hash(i + vec3(1,0,0)), f.x),
              mix(hash(i + vec3(0,1,0)), hash(i + vec3(1,1,0)), f.x), f.y),
          mix(mix(hash(i + vec3(0,0,1)), hash(i + vec3(1,0,1)), f.x),
              mix(hash(i + vec3(0,1,1)), hash(i + vec3(1,1,1)), f.x), f.y),
          f.z
        );
      }

      // Fractal Brownian Motion for nebula clouds
      // (4 octaves — the 5th cost a full noise3 per fragment for detail
      // that's invisible at sky distance behind the smoothstep threshold)
      float fbm(vec3 p) {
        float v = 0.0;
        float a = 0.5;
        for (int i = 0; i < 4; i++) {
          v += a * noise3(p);
          p *= 2.0;
          a *= 0.5;
        }
        return v;
      }

      // --- Star field ---
      float starField(vec3 dir, float scale, float threshold) {
        // Quantize direction to a grid on a sphere
        vec3 p = dir * scale;
        vec3 cell = floor(p);
        vec3 f = fract(p);

        float star = 0.0;
        // Check 3x3x3 neighborhood for nearest star
        for (int x = -1; x <= 1; x++) {
          for (int y = -1; y <= 1; y++) {
            for (int z = -1; z <= 1; z++) {
              vec3 neighbor = cell + vec3(float(x), float(y), float(z));
              // Random position within the cell
              vec3 starPos = neighbor + vec3(
                hash(neighbor),
                hash(neighbor + 100.0),
                hash(neighbor + 200.0)
              );
              float d = length(p - starPos);
              float brightness = hash(neighbor + 300.0);
              if (brightness > threshold) {
                float size = 0.015 + 0.025 * hash(neighbor + 400.0);
                // Twinkling
                float twinkle = 0.7 + 0.3 * sin(time * (1.0 + hash(neighbor + 500.0) * 3.0) + hash(neighbor + 600.0) * 6.28);
                float s = smoothstep(size, 0.0, d) * twinkle;
                // Glow for brighter stars
                if (brightness > 0.95) {
                  s += 0.3 * smoothstep(size * 6.0, 0.0, d) * twinkle;
                }
                star = max(star, s);
              }
            }
          }
        }
        return star;
      }

      void main() {
        vec3 dir = normalize(vDir);

        // Base color: very dark blue-black
        vec3 color = vec3(0.002, 0.003, 0.012);

        // --- Nebula layers ---
        // Purple/blue nebula band
        float n1 = fbm(dir * 3.0 + vec3(42.0, 0.0, 17.0));
        float n2 = fbm(dir * 2.0 + vec3(0.0, 33.0, 0.0));
        float nebula1 = smoothstep(0.4, 0.7, n1) * 0.15;
        float nebula2 = smoothstep(0.45, 0.75, n2) * 0.1;
        color += vec3(0.08, 0.02, 0.15) * nebula1; // purple
        color += vec3(0.02, 0.05, 0.12) * nebula2; // blue

        // Warm nebula accent
        float n3 = fbm(dir * 4.0 + vec3(100.0, 50.0, 75.0));
        float nebula3 = smoothstep(0.5, 0.8, n3) * 0.08;
        color += vec3(0.12, 0.04, 0.02) * nebula3; // warm red-orange

        // --- Stars at two scales ---
        // (each starField call walks a 3x3x3 cell neighborhood, so the
        // mid 40.0 layer was dropped — dense + sparse covers the range and
        // it's a third of the heaviest loop back for integrated GPUs)
        // Dense small stars
        float s1 = starField(dir, 80.0, 0.7);
        // Sparse bright stars
        float s3 = starField(dir, 20.0, 0.9);

        // Star color temperature varies
        vec3 starColor1 = mix(vec3(0.7, 0.8, 1.0), vec3(1.0, 0.95, 0.8), hash(dir * 80.0 + 999.0));
        vec3 starColor3 = mix(vec3(0.9, 0.95, 1.0), vec3(1.0, 0.85, 0.6), hash(dir * 20.0 + 777.0));

        color += starColor1 * s1 * 0.6;
        color += starColor3 * s3 * 1.2;

        // --- Distant sun glow (optional, matching the reference) ---
        vec3 sunDir = normalize(vec3(0.5, 0.3, -0.8));
        float sunDot = max(dot(dir, sunDir), 0.0);
        color += vec3(0.15, 0.08, 0.02) * pow(sunDot, 32.0); // tight glow
        color += vec3(0.05, 0.03, 0.01) * pow(sunDot, 4.0);  // wide haze

        gl_FragColor = vec4(color, 1.0);
      }
    `;

    const skyMat = new B.ShaderMaterial('skyboxMat', this.scene, {
      vertex: 'space',
      fragment: 'space'
    }, {
      attributes: ['position'],
      uniforms: ['worldViewProjection', 'time']
    });
    skyMat.setFloat('time', 0);
    skyMat.backFaceCulling = false;

    skybox.material = skyMat;
    skybox.isPickable = false;

    // Animate twinkling + live settings check (setEnabled is cheap enough
    // to call every frame; avoids needing a separate settings-change hook).
    this.scene.registerBeforeRender(() => {
      const on = isEnabled('arenaAmbience', 'skybox');
      skybox.setEnabled(on);
      if (on) skyMat.setFloat('time', performance.now() / 1000);
    });

    this._skybox = skybox;
  }

  /** @private Space objects flying around — satellites, comets, UFOs, Elon's Tesla. */
  _createSpaceObjects() {
    const B = window.BABYLON;
    const scene = this.scene;
    const cx = this.w / 2;
    const cz = this.h / 2;
    const minDist = Math.max(this.w, this.h) * 1.5; // never closer than 1.5x arena
    const maxDist = Math.max(this.w, this.h) * 3.5;

    // --- Sprite generation on canvas ---
    const makeTexture = (name, drawFn, size = 64) => {
      const c = document.createElement('canvas');
      c.width = size; c.height = size;
      const ctx = c.getContext('2d');
      drawFn(ctx, size);
      const tex = new B.DynamicTexture(name, { width: size, height: size }, scene, false);
      tex.getContext().drawImage(c, 0, 0);
      tex.update();
      tex.hasAlpha = true;
      return tex;
    };

    // 🛰️ Satellite
    const satelliteTex = makeTexture('satTex', (ctx, s) => {
      ctx.clearRect(0, 0, s, s);
      // Body
      ctx.fillStyle = '#888';
      ctx.fillRect(s*0.35, s*0.3, s*0.3, s*0.4);
      // Solar panels
      ctx.fillStyle = '#2266cc';
      ctx.fillRect(s*0.05, s*0.38, s*0.28, s*0.24);
      ctx.fillRect(s*0.67, s*0.38, s*0.28, s*0.24);
      // Panel grid lines
      ctx.strokeStyle = '#1144aa';
      ctx.lineWidth = 1;
      for (let i = 0; i < 3; i++) {
        ctx.beginPath();
        ctx.moveTo(s*0.05 + s*0.07*i + s*0.035, s*0.38);
        ctx.lineTo(s*0.05 + s*0.07*i + s*0.035, s*0.62);
        ctx.stroke();
        ctx.beginPath();
        ctx.moveTo(s*0.67 + s*0.07*i + s*0.035, s*0.38);
        ctx.lineTo(s*0.67 + s*0.07*i + s*0.035, s*0.62);
        ctx.stroke();
      }
      // Antenna
      ctx.strokeStyle = '#aaa';
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(s*0.5, s*0.3);
      ctx.lineTo(s*0.5, s*0.12);
      ctx.stroke();
      // Dish
      ctx.beginPath();
      ctx.arc(s*0.5, s*0.12, s*0.06, 0, Math.PI*2);
      ctx.fillStyle = '#ccc';
      ctx.fill();
    });

    // ☄️ Comet — just the glowing core (particle trail IS the tail)
    const cometTex = makeTexture('cometTex', (ctx, s) => {
      ctx.clearRect(0, 0, s, s);
      // Bright core glow
      const coreGrad = ctx.createRadialGradient(s*0.5, s*0.5, 0, s*0.5, s*0.5, s*0.4);
      coreGrad.addColorStop(0, 'rgba(255,255,255,1)');
      coreGrad.addColorStop(0.2, 'rgba(220,240,255,0.9)');
      coreGrad.addColorStop(0.5, 'rgba(150,200,255,0.5)');
      coreGrad.addColorStop(1, 'rgba(80,140,255,0)');
      ctx.fillStyle = coreGrad;
      ctx.fillRect(0, 0, s, s);
    });

    // Small white circle for comet-trail particles, shared across all comet
    // respawns instead of rebuilt per trail; freed once in dispose() like
    // _pylonGlowTex (ParticleSystem.dispose() defaults to tearing down its
    // texture, so trails must always dispose with `false`).
    const trailTex = new B.DynamicTexture('cometTrailTex', 16, scene, false);
    const ptCtx = trailTex.getContext();
    const ptGrad = ptCtx.createRadialGradient(8, 8, 0, 8, 8, 8);
    ptGrad.addColorStop(0, 'rgba(255,255,255,1)');
    ptGrad.addColorStop(1, 'rgba(255,255,255,0)');
    ptCtx.fillStyle = ptGrad;
    ptCtx.fillRect(0, 0, 16, 16);
    trailTex.update();
    this._cometTrailTex = trailTex;

    // 🚗 Tesla Roadster (Starman!)
    const teslaTex = makeTexture('teslaTex', (ctx, s) => {
      ctx.clearRect(0, 0, s, s);
      // Car body — red
      ctx.fillStyle = '#cc2222';
      ctx.beginPath();
      ctx.ellipse(s*0.5, s*0.55, s*0.32, s*0.12, 0, 0, Math.PI*2);
      ctx.fill();
      // Roof
      ctx.fillStyle = '#aa1818';
      ctx.beginPath();
      ctx.ellipse(s*0.45, s*0.48, s*0.18, s*0.08, -0.1, 0, Math.PI*2);
      ctx.fill();
      // Windshield
      ctx.fillStyle = 'rgba(100,180,255,0.5)';
      ctx.beginPath();
      ctx.ellipse(s*0.55, s*0.47, s*0.08, s*0.06, 0.2, 0, Math.PI*2);
      ctx.fill();
      // Wheels
      ctx.fillStyle = '#333';
      ctx.beginPath();
      ctx.arc(s*0.3, s*0.63, s*0.06, 0, Math.PI*2);
      ctx.fill();
      ctx.beginPath();
      ctx.arc(s*0.65, s*0.63, s*0.06, 0, Math.PI*2);
      ctx.fill();
      // Starman (astronaut silhouette in driver seat)
      ctx.fillStyle = '#eee';
      ctx.beginPath();
      ctx.arc(s*0.48, s*0.4, s*0.04, 0, Math.PI*2); // helmet
      ctx.fill();
      ctx.fillStyle = '#ddd';
      ctx.fillRect(s*0.46, s*0.44, s*0.04, s*0.06); // body
      // "DON'T PANIC" on dashboard (too small but implied)
    }, 128);

    // 👽 UFO
    const ufoTex = makeTexture('ufoTex', (ctx, s) => {
      ctx.clearRect(0, 0, s, s);
      // Dome
      ctx.fillStyle = 'rgba(150,200,255,0.5)';
      ctx.beginPath();
      ctx.ellipse(s*0.5, s*0.4, s*0.12, s*0.15, 0, Math.PI, 0);
      ctx.fill();
      // Saucer body
      ctx.fillStyle = '#777';
      ctx.beginPath();
      ctx.ellipse(s*0.5, s*0.45, s*0.35, s*0.1, 0, 0, Math.PI*2);
      ctx.fill();
      // Lights along the rim
      const colors = ['#ff4444', '#44ff44', '#4444ff', '#ffff44', '#ff44ff'];
      for (let i = 0; i < 5; i++) {
        const angle = (i / 5) * Math.PI * 2 - Math.PI/2;
        const lx = s*0.5 + Math.cos(angle) * s*0.28;
        const ly = s*0.45 + Math.sin(angle) * s*0.07;
        ctx.beginPath();
        ctx.arc(lx, ly, s*0.02, 0, Math.PI*2);
        ctx.fillStyle = colors[i];
        ctx.fill();
      }
      // Beam (subtle)
      const beam = ctx.createLinearGradient(s*0.4, s*0.5, s*0.4, s*0.9);
      beam.addColorStop(0, 'rgba(100,255,100,0.15)');
      beam.addColorStop(1, 'rgba(100,255,100,0)');
      ctx.fillStyle = beam;
      ctx.beginPath();
      ctx.moveTo(s*0.35, s*0.52);
      ctx.lineTo(s*0.25, s*0.9);
      ctx.lineTo(s*0.75, s*0.9);
      ctx.lineTo(s*0.65, s*0.52);
      ctx.fill();
    });

    // 🛸 Space Debris (ISS-like)
    const debrisTex = makeTexture('debrisTex', (ctx, s) => {
      ctx.clearRect(0, 0, s, s);
      ctx.fillStyle = '#999';
      ctx.fillRect(s*0.2, s*0.45, s*0.6, s*0.1); // truss
      ctx.fillStyle = '#336699';
      ctx.fillRect(s*0.1, s*0.35, s*0.15, s*0.3); // panel L
      ctx.fillRect(s*0.75, s*0.35, s*0.15, s*0.3); // panel R
      ctx.fillStyle = '#aaa';
      ctx.fillRect(s*0.4, s*0.4, s*0.2, s*0.2); // module
    });

    const objectDefs = [
      { name: 'satellite', tex: satelliteTex, size: 250, speed: [15, 35], weight: 3 },
      { name: 'comet',     tex: cometTex,     size: 400, speed: [60, 120], weight: 2 },
      { name: 'tesla',     tex: teslaTex,     size: 350, speed: [20, 40],  weight: 1 },
      { name: 'ufo',       tex: ufoTex,       size: 300, speed: [25, 55],  weight: 2 },
      { name: 'debris',    tex: debrisTex,     size: 200, speed: [10, 25], weight: 2 },
    ];

    // Build weighted pool
    const pool = [];
    for (const def of objectDefs) {
      for (let i = 0; i < def.weight; i++) pool.push(def);
    }

    // Active space objects
    this._spaceObjects = [];
    const MAX_OBJECTS = 6;

    const randomPointOnShell = () => {
      // Random point on a sphere shell between minDist and maxDist from arena center
      const dist = minDist + Math.random() * (maxDist - minDist);
      const theta = Math.random() * Math.PI * 2;
      const phi = Math.acos(2 * Math.random() - 1); // uniform sphere distribution
      return new B.Vector3(
        cx + dist * Math.sin(phi) * Math.cos(theta),
        dist * Math.sin(phi) * Math.sin(theta) * 0.6, // slightly flatten vertical
        cz + dist * Math.cos(phi)
      );
    };

    const spawnObject = (reuseMat = null) => {
      const def = pool[Math.floor(Math.random() * pool.length)];

      // Start position — random point on the outer shell
      const start = randomPointOnShell();

      // End position — opposite-ish side of the arena
      const end = randomPointOnShell();

      // Create billboard sprite
      const plane = B.MeshBuilder.CreatePlane('space_' + def.name + '_' + Date.now(), {
        width: def.size,
        height: def.size
      }, scene);

      // Reuse the slot's material on respawn — only the sprite textures
      // change between object types; the static flags are set once.
      const mat = reuseMat || new B.StandardMaterial('spMat_' + Date.now(), scene);
      mat.diffuseTexture = def.tex;
      mat.emissiveTexture = def.tex; // self-lit in space
      mat.opacityTexture = def.tex;
      if (!reuseMat) {
        mat.emissiveColor = new B.Color3(0.8, 0.8, 0.8);
        mat.backFaceCulling = false;
        mat.disableLighting = true;
      }
      plane.material = mat;
      plane.billboardMode = B.Mesh.BILLBOARDMODE_ALL;
      plane.position = start.clone();
      plane.isPickable = false;

      // Add a comet particle trail
      let trail = null;
      if (def.name === 'comet') {
        trail = new B.ParticleSystem('cometTrail_' + Date.now(), 50, scene);
        trail.emitter = plane;
        trail.minSize = 15;
        trail.maxSize = 40;
        trail.minLifeTime = 1.0;
        trail.maxLifeTime = 2.5;
        trail.emitRate = 60;
        trail.color1 = new B.Color4(0.5, 0.7, 1.0, 0.6);
        trail.color2 = new B.Color4(0.3, 0.5, 0.8, 0.3);
        trail.colorDead = new B.Color4(0.1, 0.2, 0.5, 0);
        trail.direction1 = new B.Vector3(-1, -1, -1);
        trail.direction2 = new B.Vector3(1, 1, 1);
        trail.minEmitPower = 1;
        trail.maxEmitPower = 3;
        trail.gravity = new B.Vector3(0, 0, 0);
        trail.blendMode = B.ParticleSystem.BLENDMODE_ADD;
        trail.particleTexture = this._cometTrailTex; // shared — see hoist above
        trail.start();
      }

      // UFO gets a slight wobble
      const wobble = def.name === 'ufo' ? 0.3 + Math.random() * 0.5 : 0;

      const speed = def.speed[0] + Math.random() * (def.speed[1] - def.speed[0]);
      const dir = end.subtract(start).normalize();
      const totalDist = B.Vector3.Distance(start, end);
      const duration = totalDist / speed;

      return { plane, mat, trail, start, dir, speed, duration, elapsed: 0, wobble, def };
    };

    // Spawn initial batch (staggered)
    for (let i = 0; i < MAX_OBJECTS; i++) {
      const obj = spawnObject();
      obj.elapsed = Math.random() * obj.duration; // random progress so they don't all start at once
      obj.plane.position = obj.start.add(obj.dir.scale(obj.speed * obj.elapsed));
      this._spaceObjects.push(obj);
    }

    const COMET_TRAIL_EMIT_RATE = 60;

    // Animation loop
    scene.registerBeforeRender(() => {
      const on = isEnabled('arenaAmbience', 'spaceObjects');
      if (!on) {
        // Hide and freeze in place rather than disposing/skipping elapsed
        // advance — keeps respawn timing sane for when it's re-enabled.
        for (const obj of this._spaceObjects) {
          obj.plane.setEnabled(false);
          if (obj.trail) obj.trail.emitRate = 0;
        }
        return;
      }

      const dt = scene.getEngine().getDeltaTime() / 1000;
      for (let i = 0; i < this._spaceObjects.length; i++) {
        const obj = this._spaceObjects[i];
        obj.plane.setEnabled(true);
        if (obj.trail) obj.trail.emitRate = COMET_TRAIL_EMIT_RATE;
        obj.elapsed += dt;
        // Mutate the existing position vector in place — .add()/.scale()
        // here allocated 2 fresh Vector3 per object per frame.
        obj.plane.position.copyFrom(obj.start);
        obj.dir.scaleAndAddToRef(obj.speed * obj.elapsed, obj.plane.position);

        // UFO wobble
        if (obj.wobble > 0) {
          obj.plane.position.y += Math.sin(obj.elapsed * 3) * obj.wobble * 20;
        }

        // Rotate satellites and debris slowly
        if (obj.def.name === 'satellite' || obj.def.name === 'debris') {
          obj.plane.rotation.z = obj.elapsed * 0.3;
        }

        // Tesla tumbles slowly (like the real one!)
        if (obj.def.name === 'tesla') {
          obj.plane.rotation.z = obj.elapsed * 0.15;
        }



        // Respawn when it's traveled its full path
        if (obj.elapsed >= obj.duration) {
          // Cleanup — the material is handed back to spawnObject() for
          // reuse, and the trail texture is the shared _cometTrailTex
          // (hence dispose(false); see the hoist above).
          obj.plane.dispose();
          if (obj.trail) { obj.trail.dispose(false); }

          // Replace with new object
          this._spaceObjects[i] = spawnObject(obj.mat);
        }
      }
    });
  }

  /** @private Transparent arena floor with soft ambient energy motion. */
  _createFloor() {
    const B = window.BABYLON;
    const ground = B.MeshBuilder.CreateGround('ground', {
      width: this.w, height: this.h, subdivisions: 4
    }, this.scene);
    ground.position.x = this.w / 2;
    ground.position.z = this.h / 2;

    const floorCanvas = document.createElement('canvas');
    floorCanvas.width = 1024;
    floorCanvas.height = 1024;
    const ctx = floorCanvas.getContext('2d');
    const grad = ctx.createRadialGradient(512, 512, 90, 512, 512, 640);
    grad.addColorStop(0, 'rgba(10,20,38,0.35)');
    grad.addColorStop(0.55, 'rgba(6,12,22,0.2)');
    grad.addColorStop(1, 'rgba(2,4,8,0.05)');
    ctx.fillStyle = grad;
    ctx.fillRect(0, 0, 1024, 1024);

    for (let i = 0; i < 2600; i++) {
      const x = Math.random() * 1024;
      const y = Math.random() * 1024;
      const r = 0.6 + Math.random() * 2.2;
      const a = 0.018 + Math.random() * 0.05;
      ctx.beginPath();
      ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fillStyle = `rgba(${80 + (Math.random() * 50 | 0)},${130 + (Math.random() * 60 | 0)},255,${a.toFixed(3)})`;
      ctx.fill();
    }

    for (let i = 0; i < 26; i++) {
      const x = Math.random() * 1024;
      const y = Math.random() * 1024;
      const w = 80 + Math.random() * 180;
      const h = 40 + Math.random() * 110;
      const g = ctx.createRadialGradient(x, y, 0, x, y, Math.max(w, h));
      g.addColorStop(0, 'rgba(40,120,255,0.08)');
      g.addColorStop(0.45, 'rgba(80,180,255,0.03)');
      g.addColorStop(1, 'rgba(0,0,0,0)');
      ctx.fillStyle = g;
      ctx.fillRect(x - w, y - h, w * 2, h * 2);
    }

    const floorTex = new B.DynamicTexture('floorDeckTex', { width: 1024, height: 1024 }, this.scene, false);
    floorTex.getContext().drawImage(floorCanvas, 0, 0);
    floorTex.update();

    const mat = new B.StandardMaterial('floorMat', this.scene);
    mat.diffuseTexture = floorTex;
    mat.emissiveTexture = floorTex;
    mat.diffuseColor = new B.Color3(0.12, 0.18, 0.28);
    mat.emissiveColor = new B.Color3(0.08, 0.14, 0.24);
    mat.specularColor = new B.Color3(0.05, 0.07, 0.1);
    mat.alpha = 0.34;
    mat.backFaceCulling = false;

    ground.material = mat;
    ground.isPickable = false;
    ground.receiveShadows = true;
    ground.freezeWorldMatrix();

    // Add a second layer — very soft ambient energy motion with no grid structure.
    const glow = B.MeshBuilder.CreateGround('floorGlow', {
      width: this.w, height: this.h, subdivisions: 2
    }, this.scene);
    glow.position.set(this.w / 2, 0.24, this.h / 2);

    B.Effect.ShadersStore['energyFloorVertexShader'] = `
      precision highp float;
      attribute vec3 position;
      attribute vec2 uv;
      uniform mat4 worldViewProjection;
      varying vec2 vUV;
      void main() {
        gl_Position = worldViewProjection * vec4(position, 1.0);
        vUV = uv;
      }
    `;
    B.Effect.ShadersStore['energyFloorFragmentShader'] = `
      precision highp float;
      varying vec2 vUV;
      uniform float time;
      void main() {
        vec2 p = vUV - 0.5;
        float dist = length(p);
        float swirl = 0.5 + 0.5 * sin((dist * 16.0 - time * 0.8) + sin(vUV.x * 5.5 + time * 0.16) * 1.5);
        float cloud = 0.5 + 0.5 * sin(vUV.x * 9.0 + time * 0.1) * sin(vUV.y * 7.0 - time * 0.14);
        float basin = smoothstep(0.92, 0.12, dist);
        float edge = smoothstep(0.3, 0.5, abs(vUV.x - 0.5)) + smoothstep(0.3, 0.5, abs(vUV.y - 0.5));

        float energy = basin * (swirl * 0.06 + cloud * 0.03);
        vec3 color = vec3(0.04, 0.14, 0.34) * energy;
        color += vec3(0.02, 0.06, 0.14) * edge * 0.05;

        float alpha = energy * 0.55 + edge * 0.018;
        gl_FragColor = vec4(color, alpha);
      }
    `;

    const glowMat = new B.ShaderMaterial('energyFloorMat', this.scene, {
      vertex: 'energyFloor',
      fragment: 'energyFloor'
    }, {
      attributes: ['position', 'uv'],
      uniforms: ['worldViewProjection', 'time'],
      needAlphaBlending: true
    });
    glowMat.setFloat('time', 0);
    glowMat.backFaceCulling = false;
    glowMat.alphaMode = B.Engine.ALPHA_ADD;

    glow.material = glowMat;
    glow.isPickable = false;

    // Gate ONLY this glow overlay layer — the base textured ground mesh
    // above is the actual floor and stays on regardless of this setting.
    this.scene.registerBeforeRender(() => {
      const on = isEnabled('arenaAmbience', 'floorEnergyGlow');
      glow.setEnabled(on);
      if (on) glowMat.setFloat('time', performance.now() / 1000);
    });

    this._ground = ground;
    this._floorGlow = glow;
  }

  /** @private Perimeter walls — thick energy barriers. */
  _createWalls() {
    const B = window.BABYLON;
    const wallH = 50, wallD = 20; // much thicker and taller

    // Semi-transparent energy wall material
    const wallMat = new B.StandardMaterial('wallMat', this.scene);
    wallMat.diffuseColor = new B.Color3(0.04, 0.08, 0.16);
    wallMat.emissiveColor = new B.Color3(0.05, 0.12, 0.26);
    wallMat.specularColor = new B.Color3(0.12, 0.24, 0.46);
    wallMat.alpha = 0.72;
    wallMat.backFaceCulling = false;
    wallMat.freeze();

    // Bright edge trim on top of walls
    const trimMat = new B.StandardMaterial('trimMat', this.scene);
    trimMat.diffuseColor = B.Color3.Black();
    trimMat.emissiveColor = new B.Color3(0.24, 0.72, 1.0);
    trimMat.disableLighting = true;
    trimMat.freeze();

    const specs = [
      [this.w / 2, 0, this.w + wallD, wallD],
      [this.w / 2, this.h, this.w + wallD, wallD],
      [0, this.h / 2, wallD, this.h + wallD],
      [this.w, this.h / 2, wallD, this.h + wallD],
    ];
    // Collected so setupShadows() can register them as static casters.
    this._walls = [];
    for (let i = 0; i < specs.length; i++) {
      const [cx, cz, bw, bd] = specs[i];
      // Main wall body
      const wall = B.MeshBuilder.CreateBox(`wall-${i}`, {
        width: bw, height: wallH, depth: bd
      }, this.scene);
      wall.position.set(cx, wallH / 2, cz);
      wall.material = wallMat;
      wall.isPickable = false;
      wall.freezeWorldMatrix();
      this._walls.push(wall);

      // Glowing trim strip on top
      const trim = B.MeshBuilder.CreateBox(`trim-${i}`, {
        width: bw + 2, height: 3, depth: bd + 2
      }, this.scene);
      trim.position.set(cx, wallH + 1.5, cz);
      trim.material = trimMat;
      trim.isPickable = false;
      trim.freezeWorldMatrix();
    }
  }

  /** @private Corner pylons — glowing energy pillars with upward beams. */
  _createCornerPylons() {
    const B = window.BABYLON;
    const pylonH = 120;
    const corners = [
      [0, 0], [this.w, 0], [0, this.h], [this.w, this.h]
    ];

    // Pylon material — dark metal with blue emissive
    const pylonMat = new B.StandardMaterial('pylonMat', this.scene);
    pylonMat.diffuseColor = new B.Color3(0.08, 0.08, 0.12);
    pylonMat.emissiveColor = new B.Color3(0.05, 0.15, 0.3);
    pylonMat.specularColor = new B.Color3(0.3, 0.5, 0.8);
    pylonMat.freeze();

    // Beacon light material — bright blue glow
    const beaconMat = new B.StandardMaterial('beaconMat', this.scene);
    beaconMat.diffuseColor = B.Color3.Black();
    beaconMat.emissiveColor = new B.Color3(0.3, 0.7, 1.0);
    beaconMat.disableLighting = true;
    beaconMat.freeze();

    // Glow circle particle texture, shared across all 4 pylon beams; freed
    // once in dispose() rather than per-beam (ParticleSystem.dispose()
    // defaults to tearing down its texture, which would blank the rest).
    const glowTex = new B.DynamicTexture('pylonGlowTex', 32, this.scene, false);
    const gctx = glowTex.getContext();
    const grd = gctx.createRadialGradient(16, 16, 0, 16, 16, 16);
    grd.addColorStop(0, 'rgba(255,255,255,1)');
    grd.addColorStop(0.5, 'rgba(100,180,255,0.5)');
    grd.addColorStop(1, 'rgba(0,50,150,0)');
    gctx.fillStyle = grd;
    gctx.fillRect(0, 0, 32, 32);
    glowTex.update();
    glowTex.hasAlpha = true;
    this._pylonGlowTex = glowTex;

    this._pylons = [];

    for (let i = 0; i < corners.length; i++) {
      const [cx, cz] = corners[i];

      // Main pylon — octagonal-ish cylinder
      const pylon = B.MeshBuilder.CreateCylinder(`pylon-${i}`, {
        height: pylonH, diameter: 18, tessellation: 8
      }, this.scene);
      pylon.position.set(cx, pylonH / 2, cz);
      pylon.material = pylonMat;
      pylon.isPickable = false;
      pylon.freezeWorldMatrix();

      // Beacon sphere at top
      const beacon = B.MeshBuilder.CreateSphere(`beacon-${i}`, {
        diameter: 12, segments: 8
      }, this.scene);
      beacon.position.set(cx, pylonH + 6, cz);
      beacon.material = beaconMat;
      beacon.isPickable = false;

      // Upward beam particles from the beacon
      const beam = new B.ParticleSystem(`pylonBeam-${i}`, 40, this.scene);
      beam.particleTexture = glowTex;
      beam.emitter = new B.Vector3(cx, pylonH + 10, cz);
      beam.minEmitBox = new B.Vector3(-2, 0, -2);
      beam.maxEmitBox = new B.Vector3(2, 0, 2);
      beam.direction1 = new B.Vector3(-0.1, 1, -0.1);
      beam.direction2 = new B.Vector3(0.1, 1, 0.1);
      beam.minEmitPower = 40;
      beam.maxEmitPower = 70;
      beam.minLifeTime = 1.0;
      beam.maxLifeTime = 2.0;
      beam.minSize = 5;
      beam.maxSize = 12;
      const PYLON_BEAM_EMIT_RATE = 15;
      beam.emitRate = PYLON_BEAM_EMIT_RATE;
      beam.gravity = new B.Vector3(0, 5, 0);
      beam.color1 = new B.Color4(0.2, 0.5, 1.0, 0.7);
      beam.color2 = new B.Color4(0.1, 0.3, 0.8, 0.4);
      beam.colorDead = new B.Color4(0.05, 0.15, 0.5, 0);
      beam.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      beam.start();

      // Base ring glow
      const ring = B.MeshBuilder.CreateTorus(`pylonRing-${i}`, {
        diameter: 28, thickness: 2, tessellation: 24
      }, this.scene);
      ring.position.set(cx, 1, cz);
      ring.material = beaconMat;
      ring.isPickable = false;
      ring.freezeWorldMatrix();

      this._pylons.push({ pylon, beacon, beam, ring, baseBeamEmitRate: PYLON_BEAM_EMIT_RATE });
    }

    // Animate beacon pulse + live settings check (beams, beacons, pylon
    // bodies, and base rings are one "corner pylons" toggle).
    let t = 0;
    this.scene.registerBeforeRender(() => {
      t += this.scene.getEngine().getDeltaTime() / 1000;
      const pulse = 0.7 + 0.3 * Math.sin(t * 2);
      const on = isEnabled('arenaAmbience', 'cornerPylons');
      for (const p of this._pylons) {
        p.pylon.setEnabled(on);
        p.beacon.setEnabled(on);
        p.ring.setEnabled(on);
        p.beam.emitRate = on ? p.baseBeamEmitRate : 0;
        if (!on) continue;
        p.beacon.scaling.setAll(pulse);
      }
    });
  }

  /** @private Anti-gravity thrusters under the arena — particle jets pushing down. */
  _createUndersideThrusters() {
    const B = window.BABYLON;

    // Glow texture for thruster particles, shared across all jets; see
    // note on glowTex above about deferring texture disposal.
    const thrustTex = new B.DynamicTexture('thrustTex', 32, this.scene, false);
    const tctx = thrustTex.getContext();
    const tgrd = tctx.createRadialGradient(16, 16, 0, 16, 16, 16);
    tgrd.addColorStop(0, 'rgba(255,200,100,1)');
    tgrd.addColorStop(0.3, 'rgba(255,120,50,0.6)');
    tgrd.addColorStop(0.7, 'rgba(100,50,200,0.3)');
    tgrd.addColorStop(1, 'rgba(50,20,100,0)');
    tctx.fillStyle = tgrd;
    tctx.fillRect(0, 0, 32, 32);
    thrustTex.update();
    thrustTex.hasAlpha = true;
    this._thrusterGlowTex = thrustTex;

    // Place 4 thrusters in a diamond pattern under the arena
    const positions = [
      [this.w * 0.25, this.h * 0.5],
      [this.w * 0.75, this.h * 0.5],
      [this.w * 0.5, this.h * 0.25],
      [this.w * 0.5, this.h * 0.75],
    ];

    this._thrusters = [];

    for (let i = 0; i < positions.length; i++) {
      const [px, pz] = positions[i];

      // Thruster nozzle cone (under the floor, pointing down)
      const nozzle = B.MeshBuilder.CreateCylinder(`thruster-${i}`, {
        height: 15, diameterTop: 10, diameterBottom: 35, tessellation: 12
      }, this.scene);
      nozzle.position.set(px, -12, pz);
      const hMat = new B.StandardMaterial(`thrusterMat-${i}`, this.scene);
      hMat.diffuseColor = new B.Color3(0.1, 0.1, 0.15);
      hMat.emissiveColor = new B.Color3(0.15, 0.05, 0.02);
      hMat.freeze();
      nozzle.material = hMat;
      nozzle.isPickable = false;
      nozzle.freezeWorldMatrix();

      // Inner glow ring at nozzle opening
      const glow = B.MeshBuilder.CreateTorus(`thrusterGlow-${i}`, {
        diameter: 30, thickness: 3, tessellation: 16
      }, this.scene);
      glow.position.set(px, -20, pz);
      const glowMat = new B.StandardMaterial(`thrustGlowMat-${i}`, this.scene);
      glowMat.diffuseColor = B.Color3.Black();
      glowMat.emissiveColor = new B.Color3(1.0, 0.5, 0.1);
      glowMat.disableLighting = true;
      glowMat.freeze();
      glow.material = glowMat;
      glow.isPickable = false;
      glow.freezeWorldMatrix();

      // Downward particle jet — emits from BELOW the nozzle
      const jet = new B.ParticleSystem(`thrusterJet-${i}`, 80, this.scene);
      jet.particleTexture = thrustTex;
      jet.emitter = new B.Vector3(px, -22, pz);
      jet.minEmitBox = new B.Vector3(-6, 0, -6);
      jet.maxEmitBox = new B.Vector3(6, 0, 6);
      jet.direction1 = new B.Vector3(-0.2, -1, -0.2);
      jet.direction2 = new B.Vector3(0.2, -1, 0.2);
      jet.minEmitPower = 40;
      jet.maxEmitPower = 80;
      jet.minLifeTime = 0.4;
      jet.maxLifeTime = 1.0;
      jet.minSize = 10;
      jet.maxSize = 25;
      const THRUSTER_JET_EMIT_RATE = 35;
      jet.emitRate = THRUSTER_JET_EMIT_RATE;
      jet.gravity = new B.Vector3(0, -15, 0);
      jet.color1 = new B.Color4(1.0, 0.6, 0.2, 0.9);
      jet.color2 = new B.Color4(0.6, 0.3, 0.8, 0.5);
      jet.colorDead = new B.Color4(0.3, 0.1, 0.4, 0);
      jet.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      jet.start();

      this._thrusters.push({ nozzle, glow, jet, baseJetEmitRate: THRUSTER_JET_EMIT_RATE });
    }

    // Live settings check — nozzle housing, glow ring, and jet particles
    // are one "underside thrusters" toggle.
    this.scene.registerBeforeRender(() => {
      const on = isEnabled('arenaAmbience', 'thrusters');
      for (const t of this._thrusters) {
        t.nozzle.setEnabled(on);
        t.glow.setEnabled(on);
        t.jet.emitRate = on ? t.baseJetEmitRate : 0;
      }
    });
  }

  /** @private Energy waterfalls cascading off the arena edges. */
  _createEdgeWaterfalls() {
    const B = window.BABYLON;

    // Particle texture — soft blue glow, shared across all waterfalls; see
    // note on glowTex above about deferring texture disposal.
    const dropTex = new B.DynamicTexture('dropTex', 16, this.scene, false);
    const dctx = dropTex.getContext();
    const dgrd = dctx.createRadialGradient(8, 8, 0, 8, 8, 8);
    dgrd.addColorStop(0, 'rgba(100,180,255,1)');
    dgrd.addColorStop(0.5, 'rgba(50,100,200,0.5)');
    dgrd.addColorStop(1, 'rgba(20,50,150,0)');
    dctx.fillStyle = dgrd;
    dctx.fillRect(0, 0, 16, 16);
    dropTex.update();
    dropTex.hasAlpha = true;
    this._waterfallDropTex = dropTex;

    // One waterfall per edge, emitting from the wall top and falling down
    const edges = [
      { pos: [this.w / 2, 0],   box: [this.w * 0.8, 0, 0, 2] },  // north
      { pos: [this.w / 2, this.h], box: [this.w * 0.8, 0, 0, 2] },  // south
      { pos: [0, this.h / 2],   box: [0, 0, 2, this.h * 0.8] },  // west
      { pos: [this.w, this.h / 2], box: [0, 0, 2, this.h * 0.8] },  // east
    ];

    this._waterfalls = [];
    const wallH = 50;
    const WATERFALL_EMIT_RATE = 20;

    for (let i = 0; i < edges.length; i++) {
      const e = edges[i];
      const ps = new B.ParticleSystem(`waterfall-${i}`, 100, this.scene);
      ps.particleTexture = dropTex;
      ps.emitter = new B.Vector3(e.pos[0], wallH - 5, e.pos[1]);
      ps.minEmitBox = new B.Vector3(-e.box[0]/2, -2, -e.box[3]/2);
      ps.maxEmitBox = new B.Vector3(e.box[0]/2, 2, e.box[3]/2);
      ps.direction1 = new B.Vector3(-0.1, -1, -0.1);
      ps.direction2 = new B.Vector3(0.1, -0.5, 0.1);
      ps.minEmitPower = 5;
      ps.maxEmitPower = 15;
      ps.minLifeTime = 1.5;
      ps.maxLifeTime = 3.0;
      ps.minSize = 2;
      ps.maxSize = 6;
      ps.emitRate = WATERFALL_EMIT_RATE;
      ps.gravity = new B.Vector3(0, -20, 0);
      ps.color1 = new B.Color4(0.2, 0.5, 1.0, 0.6);
      ps.color2 = new B.Color4(0.1, 0.3, 0.8, 0.3);
      ps.colorDead = new B.Color4(0.05, 0.15, 0.5, 0);
      ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;
      ps.start();
      this._waterfalls.push(ps);
    }

    // Live settings check — zero the emit rate rather than stop() so a
    // toggle takes effect immediately without losing emitter state.
    this.scene.registerBeforeRender(() => {
      const rate = isEnabled('arenaAmbience', 'edgeWaterfalls') ? WATERFALL_EMIT_RATE : 0;
      for (const ps of this._waterfalls) ps.emitRate = rate;
    });
  }

  /** @private Rotating energy rings around corner pylons. */
  _createPylonRings() {
    const B = window.BABYLON;
    const corners = [
      [0, 0], [this.w, 0], [0, this.h], [this.w, this.h]
    ];

    const ringMat = new B.StandardMaterial('pylonRingMat', this.scene);
    ringMat.diffuseColor = B.Color3.Black();
    ringMat.emissiveColor = new B.Color3(0.15, 0.45, 0.9);
    ringMat.alpha = 0.5;
    ringMat.disableLighting = true;
    ringMat.backFaceCulling = false;
    ringMat.freeze();

    this._pylonRings = [];

    for (let i = 0; i < corners.length; i++) {
      const [cx, cz] = corners[i];

      // Two rings per pylon at different heights, rotating opposite directions
      const ring1 = B.MeshBuilder.CreateTorus(`pylonOrbit1-${i}`, {
        diameter: 50, thickness: 1.5, tessellation: 32
      }, this.scene);
      ring1.position.set(cx, 60, cz);
      ring1.material = ringMat;
      ring1.isPickable = false;

      const ring2 = B.MeshBuilder.CreateTorus(`pylonOrbit2-${i}`, {
        diameter: 40, thickness: 1, tessellation: 24
      }, this.scene);
      ring2.position.set(cx, 90, cz);
      ring2.rotation.x = 0.4;
      ring2.material = ringMat;
      ring2.isPickable = false;

      this._pylonRings.push({ ring1, ring2, cx, cz });
    }

    // Animate rotation + live settings check. Not its own toggle in the
    // schema — these orbit rings decorate the same corner-pylon structures
    // gated by _createCornerPylons(), so they ride the 'cornerPylons' key
    // too (leaving them on while the pylons vanish would look broken).
    this.scene.registerBeforeRender(() => {
      const on = isEnabled('arenaAmbience', 'cornerPylons');
      const dt = this.scene.getEngine().getDeltaTime() / 1000;
      for (const pr of this._pylonRings) {
        pr.ring1.setEnabled(on);
        pr.ring2.setEnabled(on);
        if (!on) continue;
        pr.ring1.rotation.y += dt * 0.8;
        pr.ring1.rotation.x = Math.sin(pr.ring1.rotation.y) * 0.3;
        pr.ring2.rotation.y -= dt * 1.2;
      }
    });
  }

  /** @private Holographic arena title floating above. */
  _createHoloTitle() {
    const B = window.BABYLON;

    // Create text texture
    const texW = 1024, texH = 128;
    const tex = new B.DynamicTexture('holoTitleTex', { width: texW, height: texH }, this.scene, false);
    tex.hasAlpha = true;
    const ctx = tex.getContext();
    ctx.clearRect(0, 0, texW, texH);
    ctx.font = 'bold 72px monospace';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    // Outer glow
    ctx.shadowColor = 'rgba(50,150,255,0.8)';
    ctx.shadowBlur = 20;
    ctx.fillStyle = 'rgba(100,200,255,0.9)';
    ctx.fillText('AI BATTLE ARENA', texW/2, texH/2);
    // Second pass for brightness
    ctx.shadowBlur = 10;
    ctx.fillStyle = 'rgba(200,230,255,0.7)';
    ctx.fillText('AI BATTLE ARENA', texW/2, texH/2);
    tex.update();

    // Sized and raised to clear the corner-pylon beams at overview zoom.
    const plane = B.MeshBuilder.CreatePlane('holoTitle', {
      width: 520, height: 65
    }, this.scene);
    plane.position.set(this.w / 2, 200, this.h / 2);
    plane.billboardMode = B.Mesh.BILLBOARDMODE_ALL;
    plane.isPickable = false;

    const mat = new B.StandardMaterial('holoTitleMat', this.scene);
    mat.diffuseTexture = tex;
    mat.emissiveTexture = tex;
    mat.emissiveColor = new B.Color3(0.5, 0.7, 1.0);
    mat.opacityTexture = tex;
    mat.disableLighting = true;
    mat.backFaceCulling = false;
    mat.alpha = 0.85;
    plane.material = mat;

    // Subtle float animation (observer stored so dispose() can remove it —
    // the arena-resize rebuild path must not leak per-frame work).
    const baseY = 200;
    this._holoTitleObs = this.scene.onBeforeRenderObservable.add(() => {
      // Portrait phones: Babylon's vertical-fixed FOV shows only ~380 world
      // units of width at the default zoom, so this 520-unit banner would
      // span well past both screen edges. Landscape/desktop only.
      const eng = this.scene.getEngine();
      const on = isEnabled('arenaAmbience', 'holoTitle') &&
        eng.getRenderWidth() >= eng.getRenderHeight();
      plane.setEnabled(on);
      if (!on) return;
      const t = performance.now() / 1000;
      plane.position.y = baseY + Math.sin(t * 0.5) * 5;
      // Subtle pulse
      mat.alpha = 0.75 + 0.1 * Math.sin(t * 1.5);
    });

    this._holoTitle = plane;
    this._holoTitleTex = tex;
    this._holoTitleMat = mat;
  }

  /** @private Ambient floating dust/ember particles for atmosphere. */
  _createAmbientParticles() {
    const B = window.BABYLON;
    const ps = new B.ParticleSystem('ambientParticles', 25, this.scene);

    // Use default particle texture (white circle)
    ps.particleTexture = new B.Texture(
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAAAXNSR0IArs4c6QAAAYhJREFUWEftlr1OwzAQx/+XtCJiYGBhYeUFeAd4Bx6BhYGBBYmJhRdgZYCJB+AdeAcWBgYGBqQKpXHk+3DO51zSVEhYSk7+3P3u/OfMGBk/8P7r4BRAP8rAs45cM6x3W5xeXmJ6XSKLMvw+vqK+/t7LBaLHxF1AZIkQZqmODk5wXQ6RZ7n+HcA5nn+XQDHx8e4uLjAbDbDZDJBkiSfBuA7MUkSnJ2d4fj4+NMAvBPL5RJHR0c4Pz9HmqZIU38xDj+8EwDOOS4uLrBarbBer3FzcyMAXq6usFgssFqtsFwuP13YNwqYc47tdovtdou7uzu8vLyIAO/v7/jMAnQB/kcR/IzjUQB8DPhnAbiUA8DhwQEm4zEODw9FmcP8fr+Pz+czTCYT7O3t4e3tTfws+z3BNADYH2Ecx9jZ2cHm5qY4S9NYBOj3+5hOp9jY2EB5fY3ZbIbFYiHugTAMxWeA0WiE4XCIwWCA4XCI8XiMbrcr7oHJZIJ+v4/RaITxeIx+vy/+DYeD/yvy7wC+ADaOYCHhWiMeAAAAAElFTkSuQmCC',
      this.scene
    );

    // Box emitter covering the arena center area
    const emitW = Math.min(this.w, 1500) * 0.5;
    const emitH = Math.min(this.h, 1500) * 0.5;
    ps.emitter = new B.Vector3(this.w / 2, 5, this.h / 2);
    ps.minEmitBox = new B.Vector3(-emitW, 0, -emitH);
    ps.maxEmitBox = new B.Vector3(emitW, 40, emitH);

    // Small particles
    ps.minSize = 0.5;
    ps.maxSize = 1.5;

    // Slow upward movement
    ps.minEmitPower = 1;
    ps.maxEmitPower = 3;
    ps.direction1 = new B.Vector3(-0.3, 1, -0.3);
    ps.direction2 = new B.Vector3(0.3, 1, 0.3);

    // Long lifetime
    ps.minLifeTime = 3;
    ps.maxLifeTime = 6;

    // Emission rate — slow and sparse
    const AMBIENT_EMIT_RATE = 5;
    ps.emitRate = AMBIENT_EMIT_RATE;

    // Warm amber/orange fading to transparent
    ps.color1 = new B.Color4(1.0, 0.7, 0.3, 0.4);
    ps.color2 = new B.Color4(1.0, 0.5, 0.2, 0.2);
    ps.colorDead = new B.Color4(1.0, 0.3, 0.1, 0.0);

    // Additive blending for glow effect
    ps.blendMode = B.ParticleSystem.BLENDMODE_ADD;

    // Slight gravity pull to slow upward drift
    ps.gravity = new B.Vector3(0, -0.5, 0);

    ps.start();

    // Live settings check — zero the emit rate rather than stop() so a
    // toggle takes effect immediately without losing emitter state.
    this.scene.registerBeforeRender(() => {
      ps.emitRate = isEnabled('arenaAmbience', 'ambientParticles') ? AMBIENT_EMIT_RATE : 0;
    });

    this._ambientParticles = ps;
  }

  /** @private Create reusable materials for zone rings. */
  _initZoneMaterials() {
    const B = window.BABYLON;
    // Current zone boundary — electric blue
    this._zoneMat = new B.StandardMaterial('zoneRingMat', this.scene);
    this._zoneMat.emissiveColor = new B.Color3(0.1, 0.5, 1.0);
    this._zoneMat.diffuseColor = new B.Color3(0, 0, 0);
    this._zoneMat.specularColor = B.Color3.Black();
    this._zoneMat.disableLighting = true;
    this._zoneMat.alpha = ZONE_RING_BASE_ALPHA;
    this._zoneMat.backFaceCulling = false;

    // Target zone — dim white
    this._targetMat = new B.StandardMaterial('targetRingMat', this.scene);
    this._targetMat.emissiveColor = new B.Color3(1.0, 1.0, 1.0);
    this._targetMat.diffuseColor = new B.Color3(0, 0, 0);
    this._targetMat.specularColor = B.Color3.Black();
    this._targetMat.disableLighting = true;
    this._targetMat.alpha = 0.25;
    this._targetMat.backFaceCulling = false;

    // Clip planes bounding the arena rectangle. The zone can start larger
    // than the map (circumscribing it), so any ring geometry outside the
    // walls is clipped away — the circle only becomes visible once it
    // shrinks inside the arena.
    this._mapClipPlanes = [
      new B.Plane(-1, 0, 0, 0),        // discard x < 0
      new B.Plane(1, 0, 0, -this.w),   // discard x > w
      new B.Plane(0, 0, -1, 0),        // discard z < 0
      new B.Plane(0, 0, 1, -this.h),   // discard z > h
    ];
  }

  /** @private Clip a mesh to the arena rectangle using scene clip planes. */
  _clipToArena(mesh) {
    mesh.onBeforeRenderObservable.add(() => {
      const s = this.scene;
      const p = this._mapClipPlanes;
      s.clipPlane = p[0]; s.clipPlane2 = p[1];
      s.clipPlane3 = p[2]; s.clipPlane4 = p[3];
    });
    mesh.onAfterRenderObservable.add(() => {
      const s = this.scene;
      s.clipPlane = null; s.clipPlane2 = null;
      s.clipPlane3 = null; s.clipPlane4 = null;
    });
  }

  /** Set up shadow generator on the directional light. */
  setupShadows(light) {
    const B = window.BABYLON;
    // 2048 instead of 1024: every caster is static and the light never
    // moves, so the map below is baked once (REFRESHRATE_RENDER_ONCE), not
    // re-rendered per frame — the higher resolution is free at runtime.
    const shadowGen = new B.ShadowGenerator(2048, light);
    shadowGen.usePercentageCloserFiltering = true;
    shadowGen.bias = 0.005;
    shadowGen.normalBias = 0.02;
    shadowGen.setDarkness(0.55);
    shadowGen.getShadowMap().refreshRate = B.RenderTargetTexture.REFRESHRATE_RENDER_ONCE;
    // The boundary walls are static geometry too — let them cast.
    if (this._walls) {
      for (const wall of this._walls) shadowGen.addShadowCaster(wall);
    }
    // Ground receives shadows
    if (this._ground) {
      this._ground.receiveShadows = true;
    }
    // Live settings check — the shadowEnabled setter early-returns when the
    // value is unchanged, so a per-frame assignment costs the same as the
    // other ambience gates above.
    this.scene.registerBeforeRender(() => {
      light.shadowEnabled = isEnabled('rendering', 'shadows');
    });
    this._shadowGen = shadowGen;
  }

  /** Re-bake the frozen shadow map. Call whenever static casters change
   *  (e.g. the obstacle rebuild at a round boundary). */
  refreshShadows() {
    this._shadowGen?.getShadowMap()?.resetRefreshCounter();
  }

  /** Register a mesh as a shadow caster. */
  addShadowCaster(mesh) {
    if (this._shadowGen) {
      this._shadowGen.addShadowCaster(mesh);
    }
  }

  /**
   * Update safe zone visualization.
   * @param {Object|null} safeZone - { center: [x,y], radius, target_center: [x,y], target_radius }
   * @param {boolean} [suddenDeath] - tint the zone ring red while sudden death is active
   */
  update(safeZone, suddenDeath = false) {
    if (!safeZone) return;

    // Sudden-death tint — menacing red vs the usual electric blue. Mutate
    // the shared Color3 in place (no per-call alloc), only on transitions.
    const sd = !!suddenDeath;
    if (this._zoneSuddenDeath !== sd) {
      this._zoneSuddenDeath = sd;
      if (sd) this._zoneMat.emissiveColor.set(1.0, 0.18, 0.24);
      else this._zoneMat.emissiveColor.set(0.1, 0.5, 1.0);
    }

    const cx = safeZone.center[0];
    const cy = safeZone.center[1];
    const r = safeZone.radius;

    // Store target values for smooth lerping
    this._zoneTargetR = r;
    this._zoneTargetCx = cx;
    this._zoneTargetCy = cy;

    // Initialize current values on first call
    if (this._zoneCurR === undefined) {
      this._zoneCurR = r;
      this._zoneCurCx = cx;
      this._zoneCurCy = cy;
    }

    // Ensure ring meshes exist
    this._ensureZoneRing();
    if (safeZone.target_center) {
      this._buildTargetRing(
        safeZone.target_center[0], safeZone.target_center[1],
        safeZone.target_radius || 75
      );
    }

    // Register lerp animation if not already running
    if (!this._zoneLerpRegistered) {
      this._zoneLerpRegistered = true;
      this.scene.registerBeforeRender(() => {
        if (this._zoneTargetR === undefined || !this._zoneRing) return;
        // This ring is functionally informative (shows the shrinking safe
        // zone), not pure decoration, but still user-toggleable. Gate via
        // alpha rather than setEnabled so a spectator relying on it can
        // toggle it back on mid-round without any state loss.
        this._zoneMat.alpha = isEnabled('gameplayZoneIndicators', 'safeZoneRing') ? ZONE_RING_BASE_ALPHA : 0;
        // dt-based smoothing so convergence speed is framerate-independent
        // (a fixed per-frame factor converges 2.4x faster at 144Hz than 60Hz).
        const dt = this.scene.getEngine().getDeltaTime() / 1000;
        const lerpSpeed = 1 - Math.exp(-5 * Math.min(dt, 0.1));
        this._zoneCurR += (this._zoneTargetR - this._zoneCurR) * lerpSpeed;
        this._zoneCurCx += (this._zoneTargetCx - this._zoneCurCx) * lerpSpeed;
        this._zoneCurCy += (this._zoneTargetCy - this._zoneCurCy) * lerpSpeed;
        this._zoneRing.scaling.set(this._zoneCurR * 2, this._zoneCurR * 2, this._zoneCurR * 2);
        this._zoneRing.position.set(this._zoneCurCx, 2, this._zoneCurCy);
      });
    }
  }

  /** @private Ensure zone ring mesh exists. */
  _ensureZoneRing() {
    const B = window.BABYLON;
    if (!this._zoneRing) {
      this._zoneRing = B.MeshBuilder.CreateTorus('zoneRing', {
        diameter: 1, thickness: 0.005, tessellation: ZONE_RING_SEGMENTS
      }, this.scene);
      this._zoneRing.material = this._zoneMat;
      this._clipToArena(this._zoneRing);
    }
  }

  /** @private Build or reposition the target zone ring. */
  _buildTargetRing(cx, cy, r) {
    const B = window.BABYLON;
    if (!this._targetRing) {
      this._targetRing = B.MeshBuilder.CreateTorus('targetRing', {
        diameter: 1, thickness: 0.003, tessellation: ZONE_RING_SEGMENTS
      }, this.scene);
      this._targetRing.material = this._targetMat;
      this._clipToArena(this._targetRing);
    }
    this._targetRing.scaling.set(r * 2, r * 2, r * 2);
    this._targetRing.position.set(cx, 1, cy);
  }

  /** Dispose all environment resources. */
  dispose() {
    if (this._spaceObjects) {
      for (const obj of this._spaceObjects) {
        obj.plane.dispose(); obj.mat.dispose();
        if (obj.trail) obj.trail.dispose(false);
      }
      this._spaceObjects = null;
    }
    if (this._cometTrailTex) { this._cometTrailTex.dispose(); this._cometTrailTex = null; }
    if (this._pylons) {
      for (const p of this._pylons) { p.pylon.dispose(); p.beacon.dispose(); p.beam.dispose(false); p.ring.dispose(); }
      this._pylons = null;
    }
    if (this._pylonGlowTex) { this._pylonGlowTex.dispose(); this._pylonGlowTex = null; }
    if (this._thrusters) {
      for (const t of this._thrusters) { t.nozzle.dispose(); t.glow.dispose(); t.jet.dispose(false); }
      this._thrusters = null;
    }
    if (this._thrusterGlowTex) { this._thrusterGlowTex.dispose(); this._thrusterGlowTex = null; }
    if (this._waterfalls) {
      for (const w of this._waterfalls) w.dispose(false);
      this._waterfalls = null;
    }
    if (this._waterfallDropTex) { this._waterfallDropTex.dispose(); this._waterfallDropTex = null; }
    if (this._pylonRings) {
      for (const pr of this._pylonRings) { pr.ring1.dispose(); pr.ring2.dispose(); }
      this._pylonRings = null;
    }

    if (this._floorGlow) { this._floorGlow.dispose(); this._floorGlow = null; }
    if (this._ambientParticles) { this._ambientParticles.dispose(); this._ambientParticles = null; }

    if (this._zoneRing) { this._zoneRing.dispose(); this._zoneRing = null; }
    if (this._targetRing) { this._targetRing.dispose(); this._targetRing = null; }
    if (this._shadowGen) { this._shadowGen.dispose(); this._shadowGen = null; }
    if (this._skybox) { this._skybox.dispose(); this._skybox = null; }

    if (this._holoTitle) {
      if (this._holoTitleObs) {
        this.scene.onBeforeRenderObservable.remove(this._holoTitleObs);
        this._holoTitleObs = null;
      }
      this._holoTitleTex.dispose();
      this._holoTitleMat.dispose();
      this._holoTitle.dispose();
      this._holoTitle = null;
      this._holoTitleTex = null;
      this._holoTitleMat = null;
    }
  }
}
