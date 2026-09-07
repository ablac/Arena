# Forge character rendering

The September 2026 rebuild replaces the original primitive character shells,
weapon models, movement poses and cosmetic constructions. It retains the seven
weapon IDs, 100 three-piece cosmetic sets, 18 body forms and 24 trail IDs.
These are presentation changes; server movement, reach, damage, ownership and
subscription checks remain authoritative.

## Geometry and materials

`frontend/js/renderer/mech-geometry.js` builds closed octagonal hull sections
and boxes with actual chamfer planes. Face normals remain separate so the
warm key light and cool fill describe the manufactured edges. Hull sections
are ordered along local Y; width and depth are full dimensions. Babylon uses
clockwise triangles in the left-handed scene.

Character shells combine shaped armor with shared joint geometry. Weapons
retain their semantic hand mounts and trail-tip anchors. Cosmetic accessories
attach to the existing chest, shoulder, back and head-top anchors; full-body
forms supply their own placement metrics. Catalog strings still resolve
through fixed allowlists rather than URLs or executable model descriptions.

`forge-surfaces.js` owns the small shared graphite, gunmetal and steel maps per
scene. A temporary weapon-material clone must call `inheritForgeSurface` to
restore those shared maps and dispose Babylon's otherwise unused texture
clones. Surface detail and character lighting retain the live settings path;
never add a query tag to the shared `settings.js` import.

## Movement and inspection

Locomotion derives cadence from distance traveled and blends loaded stance,
foot swing, torso counterrotation and ankle articulation. Weapon poses retain
the contact-delay contract used by impact effects. Death, respawn, dodge,
shove and hit reactions remain in the character animation state machine.
Secondary motion respects the existing rendering toggle and reduced-motion
preference.

Open `/character-lab.html` for the Forge Studio. It displays the seven combat
chassis and all full-body forms under warm/cool studio lighting, with controls
for walking, attacks, reactions, dodging and death. The row control isolates
a group for close inspection. The production spectator and shop continue to
use the same renderer modules.

After changing geometry, run the frontend checks and the browser
`forge-rebuild.spec.mjs` case. The latter exercises the full roster and every
cosmetic set twice, checks finite transforms and stable resource counts, and
captures the seven-chassis view. Use the arena realism/lifecycle cases to
verify the models in the glass-space scene as well.
