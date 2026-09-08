# Arena overall visual polish

Keith's follow-up: "I like the start! But we can do better!" followed by "Overall polish! Arena lighting, everything". The accepted direction is a brighter orbital arena with readable glass, brushed metal, warmer bot highlights, restrained glow, stronger camera framing and clearer spectator controls.

## Observed starting point

Live screenshots in `outputs/next-visual-direction-20260907` outside the checkout show that the floor almost disappears at normal and close camera distances, cover looks like floating dark boxes, the planet has a repetitive cloud texture, and a large overhead bounty crown competes with a small bot. The UI is very dark and occupies much of the visible action area. The seven chassis have useful silhouettes but pastel armor and dark structure do not yet convey convincing metal.

## Acceptance

- The playable glass plane remains visibly connected to the structural frame at wide and close views. Soft reflected light, subtle panel seams and grounded contact cues preserve the space view below.
- Cover, perimeter and orbital background share a coherent palette and depth. No new opaque floor that hides space, no flashing spectacle, no additional per-frame mesh or texture allocation.
- All seven chassis, their weapons and cosmetics show readable material separation: dark joints, brighter brushed metal edges, restrained colored armor and luminous accents. Existing shapes and catalog compatibility remain.
- Movement and attacks retain their authored contact time and gameplay state while showing clearer weight transfer and recovery. Reduced motion and secondary-motion controls still apply.
- Automatic camera framing favors nearby combat rather than averaging fighters into empty space. It remains stable, gives manual input immediate control, respects occupied UI space and stays finite with empty, dead or malformed bot lists.
- Desktop and mobile spectator controls have clearer text, hierarchy, focus and spacing without changing their routes or account behavior.
- Quality toggles work live and preserve saved preferences. WebGPU startup/failure containment and WebGL fallback remain intact. No security, server, protocol, catalog-ID or gameplay changes.
- Verify integrated default and close views, seven chassis and representative forms/cosmetics, desktop and mobile, quality off/on, map rebuild and resource lifecycle. Run the full repository gate only after all production work is assembled, per Keith's earlier instruction.

## Scope and ownership

Environment: `environment.js`, `glass-deck.js`, `obstacles.js`, `map-walls.js`. Character finish and motion: `forge-surfaces.js`, `character-rig.js`, `character-anims.js`, `forge-weapons.js`, `cosmetics.js`, `body-form-geometry.js`. UI: `frontend/css/site-shell.css`, `frontend/css/arena.css`, `frontend/m/mobile.css`, relevant existing HUD styles only. Coordinator: `engine.js`, `camera.js`, selection display in `bot-body.js`/`bots.js`/`world-hud.js`, settings and cache import closure, integration tests and docs.

The existing scene and renderer architecture stays in place. No dependency or new backend service is needed. Source integration is local through the protected controller; GitHub publication and deployment are separate requests.
