# Frontend Performance

Arena's desktop spectator, mobile spectator, and Cosmetic Shop share a
tree-shaken Babylon.js compatibility runtime. The checked-in artifact avoids a
frontend build step in the production Go image while preserving the existing
`window.BABYLON` API used by the renderer modules.

## Babylon runtime

The source surface is `frontend/vendor-src/babylon-runtime-entry.mjs`. It pins
`@babylonjs/core` 9.14.0 and includes only the classes, mesh factories, and
runtime registrations used by Arena. Earcut is included in the same bundle and
is exposed as both `globalThis.earcut` and `BABYLON.earcut` for map
triangulation.

Generate the checked-in artifact after changing its source or dependencies:

```bash
npm ci --ignore-scripts
npm run build:babylon
```

The build hashes the exact output and writes:

- `frontend/assets/vendor/babylon-runtime.<hash>.min.js`
- `frontend/js/babylon-runtime.js`, a stable compatibility bridge

Update each `modulepreload` link in `frontend/index.html`,
`frontend/m/index.html`, and `frontend/shop/index.html` when the generated hash
changes. Do not hand-edit the minified artifact or bridge.

## Budgets and caching

`scripts/test-spectator-startup-budget.mjs` enforces one shipped runtime, a
2.30 MB raw safety ceiling, and a 500 KB Brotli cold-transfer ceiling. The
initial modular runtime measured 391,270 Brotli bytes, compared with a
1,628,804-byte response for the previous Babylon UMD asset.

Only content-hashed JavaScript under `/assets/vendor/` receives
`Cache-Control: public, max-age=31536000, immutable`. HTML, the stable bridge,
and other JavaScript keep the deployment-safe revalidation policy documented
in [Build and deploy](build-and-deploy.md).

CI rebuilds the runtime and byte-compares it with the checked-in artifact:

```bash
npm run check:babylon
node scripts/test-spectator-startup-budget.mjs
```

The Playwright smoke suite then boots the global compatibility surface on
desktop, `/m/`, and `/shop/`, and exercises multi-round WebGL lifecycle
behavior. Any new Babylon API must be added explicitly to the entry point and
covered by the relevant renderer or browser test before regenerating.
