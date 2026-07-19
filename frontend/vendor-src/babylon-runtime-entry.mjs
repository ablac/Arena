import earcut from 'earcut';

import { Animatable } from '@babylonjs/core/Animations/animatable.js';
import { Animation } from '@babylonjs/core/Animations/animation.js';
import { ArcRotateCamera } from '@babylonjs/core/Cameras/arcRotateCamera.js';
import { Color3, Color4 } from '@babylonjs/core/Maths/math.color.js';
import { Matrix, Vector3 } from '@babylonjs/core/Maths/math.vector.js';
import { Plane } from '@babylonjs/core/Maths/math.plane.js';
import { DefaultRenderingPipeline } from '@babylonjs/core/PostProcesses/RenderPipeline/Pipelines/defaultRenderingPipeline.js';
import { DirectionalLight } from '@babylonjs/core/Lights/directionalLight.js';
import { HemisphericLight } from '@babylonjs/core/Lights/hemisphericLight.js';
import { DynamicTexture } from '@babylonjs/core/Materials/Textures/dynamicTexture.js';
import { Effect } from '@babylonjs/core/Materials/effect.js';
import { Engine } from '@babylonjs/core/Engines/engine.js';
import { EngineStore } from '@babylonjs/core/Engines/engineStore.js';
import { GlowLayer } from '@babylonjs/core/Layers/glowLayer.js';
import { HighlightLayer } from '@babylonjs/core/Layers/highlightLayer.js';
import { ImageProcessingConfiguration } from '@babylonjs/core/Materials/imageProcessingConfiguration.js';
import { Material } from '@babylonjs/core/Materials/material.js';
import { Mesh } from '@babylonjs/core/Meshes/mesh.js';
// Mesh.createInstance delegates to this prototype registration.
import '@babylonjs/core/Meshes/instancedMesh.js';
import { ParticleSystem } from '@babylonjs/core/Particles/particleSystem.js';
import { PointerEventTypes } from '@babylonjs/core/Events/pointerEvents.js';
import { RenderTargetTexture } from '@babylonjs/core/Materials/Textures/renderTargetTexture.js';
import { Scene } from '@babylonjs/core/scene.js';
import { ShaderMaterial } from '@babylonjs/core/Materials/shaderMaterial.js';
import { ShadowGenerator } from '@babylonjs/core/Lights/Shadows/shadowGenerator.js';
import { StandardMaterial } from '@babylonjs/core/Materials/standardMaterial.js';
import { Texture } from '@babylonjs/core/Materials/Textures/texture.js';
import { TransformNode } from '@babylonjs/core/Meshes/transformNode.js';
import { VertexBuffer } from '@babylonjs/core/Buffers/buffer.js';
import { VertexData } from '@babylonjs/core/Meshes/mesh.vertexData.js';
import { WebGPUEngine } from '@babylonjs/core/Engines/webgpuEngine.js';

// Build the legacy MeshBuilder surface from pure factories. Importing
// Babylon's aggregate MeshBuilder would retain every unused builder.
import { CreateBox } from '@babylonjs/core/Meshes/Builders/boxBuilder.pure.js';
import { CreateCylinder } from '@babylonjs/core/Meshes/Builders/cylinderBuilder.pure.js';
import { CreateDisc } from '@babylonjs/core/Meshes/Builders/discBuilder.pure.js';
import { CreateGround } from '@babylonjs/core/Meshes/Builders/groundBuilder.pure.js';
import { CreatePlane } from '@babylonjs/core/Meshes/Builders/planeBuilder.pure.js';
import { CreatePolyhedron } from '@babylonjs/core/Meshes/Builders/polyhedronBuilder.pure.js';
import { CreateRibbon } from '@babylonjs/core/Meshes/Builders/ribbonBuilder.pure.js';
import { CreateSphere } from '@babylonjs/core/Meshes/Builders/sphereBuilder.pure.js';
import { CreateTorus } from '@babylonjs/core/Meshes/Builders/torusBuilder.pure.js';
import { CreateTorusKnot } from '@babylonjs/core/Meshes/Builders/torusKnotBuilder.pure.js';
import { CreateTube } from '@babylonjs/core/Meshes/Builders/tubeBuilder.pure.js';

const MeshBuilder = {
  CreateBox,
  CreateCylinder,
  CreateDisc,
  CreateGround,
  CreatePlane,
  CreatePolyhedron,
  CreateRibbon,
  CreateSphere,
  CreateTorus,
  CreateTorusKnot,
  CreateTube,
};

const BABYLON = {
  Animatable,
  Animation,
  ArcRotateCamera,
  Color3,
  Color4,
  DefaultRenderingPipeline,
  DirectionalLight,
  DynamicTexture,
  Effect,
  Engine,
  EngineStore,
  GlowLayer,
  HemisphericLight,
  HighlightLayer,
  ImageProcessingConfiguration,
  Material,
  Matrix,
  Mesh,
  MeshBuilder,
  ParticleSystem,
  Plane,
  PointerEventTypes,
  RenderTargetTexture,
  Scene,
  ShaderMaterial,
  ShadowGenerator,
  StandardMaterial,
  Texture,
  TransformNode,
  Vector3,
  VertexBuffer,
  VertexData,
  WebGPUEngine,
  earcut,
};

globalThis.earcut = earcut;
globalThis.BABYLON = BABYLON;
