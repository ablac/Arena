'use strict';

/**
 * Arena environment — stone floor, boundary walls, dark void outside arena.
 * @module renderer/environment
 */

import { makeMat } from './utils.js';

const GRID_SPACING = 100;

export class EnvironmentRenderer {
  /** @param {BABYLON.Scene} scene @param {number} w @param {number} h */
  constructor(scene, w, h) {
    this.scene = scene;
    this.w = w;
    this.h = h;

    this._createFloor();
    this._createWalls();
    this._createVoid();
  }

  /** @private Procedural stone floor with subtle grid. */
  _createFloor() {
    const B = window.BABYLON;
    const ground = B.MeshBuilder.CreateGround('ground', {
      width: this.w, height: this.h, subdivisions: 4
    }, this.scene);
    ground.position.x = this.w / 2;
    ground.position.z = this.h / 2;

    const size = 1024;
    const tex = new B.DynamicTexture('floorTex', size, this.scene, false);
    const ctx = tex.getContext();
    // Stone base
    ctx.fillStyle = '#2a2520';
    ctx.fillRect(0, 0, size, size);
    // Noise patches
    for (let i = 0; i < 500; i++) {
      const x = Math.random() * size, y = Math.random() * size;
      const s = 4 + Math.random() * 12;
      const v = 30 + Math.floor(Math.random() * 20);
      ctx.fillStyle = `rgb(${v + 8},${v + 4},${v})`;
      ctx.fillRect(x, y, s, s);
    }
    // Grid lines
    const cell = size / (this.w / GRID_SPACING);
    ctx.strokeStyle = 'rgba(80, 70, 55, 0.3)';
    ctx.lineWidth = 1;
    for (let i = 0; i <= size; i += cell) {
      ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, size); ctx.stroke();
      ctx.beginPath(); ctx.moveTo(0, i); ctx.lineTo(size, i); ctx.stroke();
    }
    tex.update();

    const mat = new B.StandardMaterial('floorMat', this.scene);
    mat.diffuseTexture = tex;
    mat.diffuseColor = new B.Color3(0.25, 0.22, 0.18);
    mat.specularColor = new B.Color3(0.05, 0.05, 0.05);
    mat.emissiveColor = new B.Color3(0.03, 0.025, 0.02);
    ground.material = mat;
  }

  /** @private Perimeter walls. */
  _createWalls() {
    const B = window.BABYLON;
    const wallH = 30, wallD = 6;
    const wallMat = makeMat('wallMat', this.scene, new B.Color3(0.35, 0.3, 0.25), {
      emissiveFactor: 0.15, specular: new B.Color3(0.1, 0.1, 0.1), backFace: true
    });
    const specs = [
      [this.w / 2, 0, this.w + wallD, wallD],
      [this.w / 2, this.h, this.w + wallD, wallD],
      [0, this.h / 2, wallD, this.h],
      [this.w, this.h / 2, wallD, this.h],
    ];
    for (let i = 0; i < specs.length; i++) {
      const [cx, cz, bw, bd] = specs[i];
      const wall = B.MeshBuilder.CreateBox(`wall-${i}`, {
        width: bw, height: wallH, depth: bd
      }, this.scene);
      wall.position.set(cx, wallH / 2, cz);
      wall.material = wallMat;
    }
  }

  /** @private Dark void outside arena. */
  _createVoid() {
    const B = window.BABYLON;
    const v = B.MeshBuilder.CreateGround('void', {
      width: this.w * 4, height: this.h * 4
    }, this.scene);
    v.position.set(this.w / 2, -0.5, this.h / 2);
    const mat = new B.StandardMaterial('voidMat', this.scene);
    mat.diffuseColor = new B.Color3(0.02, 0.02, 0.03);
    mat.specularColor = B.Color3.Black();
    mat.emissiveColor = new B.Color3(0.01, 0.01, 0.015);
    v.material = mat;
  }

  /** No-op — safe zones removed. */
  update() {}
}
