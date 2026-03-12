'use strict';

/**
 * Pickup rendering — glowing icons for health, speed, damage, shield.
 * @module renderer/pickups
 */

const PICKUP_RADIUS = 5;

const PICKUP_COLORS = {
  health:  [0.0, 1.0, 0.3],
  speed:   [1.0, 1.0, 0.0],
  damage:  [1.0, 0.2, 0.2],
  shield:  [0.2, 0.5, 1.0],
};

export class PickupRenderer {
  /** @param {BABYLON.Scene} scene */
  constructor(scene) {
    this.scene = scene;
    /** @type {Map<string, {mesh: BABYLON.Mesh, mat: BABYLON.StandardMaterial}>} */
    this.meshes = new Map();
    this.time = 0;
  }

  /**
   * Update pickup visuals.
   * @param {Array} pickups - [{ pickup_id, type, position: [x,y] }]
   */
  update(pickups) {
    const BABYLON = window.BABYLON;
    const seen = new Set();
    this.time += 0.05;

    for (const pickup of pickups) {
      seen.add(pickup.pickup_id);
      let entry = this.meshes.get(pickup.pickup_id);

      if (!entry) {
        entry = this._createPickup(pickup);
        this.meshes.set(pickup.pickup_id, entry);
      }

      entry.mesh.position.x = pickup.position[0];
      entry.mesh.position.z = pickup.position[1];
      // Gentle floating/pulsing effect
      entry.mesh.position.y = 2 + Math.sin(this.time + pickup.position[0]) * 1.5;
      const pulse = 0.8 + Math.sin(this.time * 2 + pickup.position[1]) * 0.2;
      entry.mesh.scaling.setAll(pulse);
    }

    // Remove despawned pickups
    for (const [id, entry] of this.meshes) {
      if (!seen.has(id)) {
        entry.mesh.dispose();
        entry.mat.dispose();
        this.meshes.delete(id);
      }
    }
  }

  /** @private */
  _createPickup(pickup) {
    const BABYLON = window.BABYLON;
    const id = pickup.pickup_id;
    const pType = pickup.type || 'health';
    const colors = PICKUP_COLORS[pType] || PICKUP_COLORS.health;

    // Diamond shape for pickups (sprite-ready placeholder)
    const mesh = BABYLON.MeshBuilder.CreateDisc(`pickup-${id}`, {
      radius: PICKUP_RADIUS, tessellation: 4
    }, this.scene);
    mesh.rotation.x = Math.PI / 2;

    const mat = new BABYLON.StandardMaterial(`mat-pickup-${id}`, this.scene);
    mat.diffuseColor = new BABYLON.Color3(colors[0], colors[1], colors[2]);
    mat.emissiveColor = new BABYLON.Color3(
      colors[0] * 0.7, colors[1] * 0.7, colors[2] * 0.7
    );
    mat.disableLighting = true;
    mat.alpha = 0.85;
    mesh.material = mat;

    return { mesh, mat };
  }
}
