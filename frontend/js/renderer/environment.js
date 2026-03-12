'use strict';

/**
 * Arena environment rendering — grid, safe zone, danger zone, obstacles.
 * @module renderer/environment
 */

const GRID_SPACING = 100;
const GRID_COLOR = [0.06, 0.08, 0.15];

export class EnvironmentRenderer {
  /**
   * @param {BABYLON.Scene} scene
   * @param {number} arenaWidth
   * @param {number} arenaHeight
   */
  constructor(scene, arenaWidth, arenaHeight) {
    this.scene = scene;
    this.arenaWidth = arenaWidth;
    this.arenaHeight = arenaHeight;
    /** @type {BABYLON.Mesh|null} */
    this.groundMesh = null;
    /** @type {BABYLON.Mesh|null} */
    this.safeZoneMesh = null;
    /** @type {BABYLON.Mesh|null} */
    this.dangerOverlay = null;
    /** @type {Map<number, BABYLON.Mesh>} */
    this.obstacleMeshes = new Map();
    this._createGround();
    this._createSafeZone();
    this._createDangerOverlay();
  }

  /** @private */
  _createGround() {
    const BABYLON = window.BABYLON;
    const ground = BABYLON.MeshBuilder.CreateGround('ground', {
      width: this.arenaWidth, height: this.arenaHeight
    }, this.scene);
    const mat = new BABYLON.StandardMaterial('groundMat', this.scene);
    mat.diffuseColor = new BABYLON.Color3(GRID_COLOR[0], GRID_COLOR[1], GRID_COLOR[2]);
    mat.specularColor = BABYLON.Color3.Black();

    // Grid texture
    const gridTex = new BABYLON.DynamicTexture('gridTex', 512, this.scene, false);
    const ctx = gridTex.getContext();
    ctx.fillStyle = `rgb(${GRID_COLOR.map(c => Math.round(c * 255)).join(',')})`;
    ctx.fillRect(0, 0, 512, 512);
    ctx.strokeStyle = 'rgba(30, 41, 59, 0.5)';
    ctx.lineWidth = 1;
    const cellSize = 512 / (this.arenaWidth / GRID_SPACING);
    for (let i = 0; i <= 512; i += cellSize) {
      ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, 512); ctx.stroke();
      ctx.beginPath(); ctx.moveTo(0, i); ctx.lineTo(512, i); ctx.stroke();
    }
    gridTex.update();
    mat.diffuseTexture = gridTex;
    mat.diffuseTexture.uScale = 1;
    mat.diffuseTexture.vScale = 1;
    ground.material = mat;
    this.groundMesh = ground;
  }

  /** @private */
  _createSafeZone() {
    const BABYLON = window.BABYLON;
    const disc = BABYLON.MeshBuilder.CreateDisc('safeZone', {
      radius: 1, tessellation: 64
    }, this.scene);
    disc.rotation.x = Math.PI / 2;
    disc.position.y = 0.2;
    const mat = new BABYLON.StandardMaterial('safeZoneMat', this.scene);
    mat.diffuseColor = new BABYLON.Color3(0, 0.5, 1);
    mat.emissiveColor = new BABYLON.Color3(0, 0.15, 0.4);
    mat.alpha = 0.12;
    mat.disableLighting = true;
    disc.material = mat;
    this.safeZoneMesh = disc;
  }

  /** @private Create red overlay for danger zone. */
  _createDangerOverlay() {
    const BABYLON = window.BABYLON;
    const plane = BABYLON.MeshBuilder.CreateGround('dangerZone', {
      width: this.arenaWidth * 2, height: this.arenaHeight * 2
    }, this.scene);
    plane.position.y = 0.1;
    const mat = new BABYLON.StandardMaterial('dangerMat', this.scene);
    mat.diffuseColor = new BABYLON.Color3(1, 0, 0);
    mat.emissiveColor = new BABYLON.Color3(0.3, 0, 0);
    mat.alpha = 0.08;
    mat.disableLighting = true;
    plane.material = mat;
    this.dangerOverlay = plane;
  }

  /**
   * Update safe zone and obstacles.
   * @param {Object} safeZone - { center: [x,y], radius }
   * @param {Array} obstacles - [{ x, y, width, height }]
   */
  update(safeZone, obstacles) {
    if (safeZone) {
      this.safeZoneMesh.position.x = safeZone.center[0];
      this.safeZoneMesh.position.z = safeZone.center[1];
      this.safeZoneMesh.scaling.x = safeZone.radius;
      this.safeZoneMesh.scaling.y = safeZone.radius;
      this.dangerOverlay.position.x = safeZone.center[0];
      this.dangerOverlay.position.z = safeZone.center[1];
    }
    if (obstacles) this._updateObstacles(obstacles);
  }

  /** @private */
  _updateObstacles(obstacles) {
    const BABYLON = window.BABYLON;
    const seen = new Set();
    obstacles.forEach((obs, i) => {
      seen.add(i);
      if (!this.obstacleMeshes.has(i)) {
        const box = BABYLON.MeshBuilder.CreateBox(`obs-${i}`, {
          width: obs.width, height: 20, depth: obs.height
        }, this.scene);
        box.position.y = 10;
        const mat = new BABYLON.StandardMaterial(`mat-obs-${i}`, this.scene);
        mat.diffuseColor = new BABYLON.Color3(0.15, 0.15, 0.2);
        mat.emissiveColor = new BABYLON.Color3(0.05, 0.05, 0.08);
        mat.specularColor = BABYLON.Color3.Black();
        box.material = mat;
        this.obstacleMeshes.set(i, box);
      }
      const mesh = this.obstacleMeshes.get(i);
      mesh.position.x = obs.x + obs.width / 2;
      mesh.position.z = obs.y + obs.height / 2;
    });
    for (const [k, mesh] of this.obstacleMeshes) {
      if (!seen.has(k)) { mesh.dispose(); this.obstacleMeshes.delete(k); }
    }
  }
}
