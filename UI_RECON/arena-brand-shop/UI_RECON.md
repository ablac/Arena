# Arena Brand Lockup UI Recon

## Input

- Source image: `screenshots/source.png`
- Design size: 2550x105
- Design size source: inferred-from-image-pixels
- Target: Arena public header, Cosmetic Shop header, Dashboard, and mobile spectator top bar
- Target platform: existing vanilla HTML/CSS Arena frontend
- Known constraints: Arena-only; the Aquarium and Angel-Serv sites are being updated separately.

## Screen Summary

- Screen type: narrow desktop application header reference.
- Primary user goal: identify the Angel Software Solutions property and its product at a glance.
- Scroll behavior: the source shows only the fixed-width header band; each Arena surface retains its existing scroll behavior.
- Responsive expectation: preserve the two-line lockup without clipping at the Arena project's existing 375, 768, and 1440 pixel verification widths.

## Layout

| Region | Bounds / proportion | Description | Evidence |
|---|---:|---|---|
| Root header | 2550x105 | A shallow, full-width dark header with content anchored to the edges. | SOURCE |
| Brand lockup | approximately first 170px | Two left-aligned text lines near the upper-left corner. | SOURCE |
| Product context | below organization name | Smaller uppercase product line with wide tracking. | SOURCE |
| Arena placement | existing top-left header locations | Reuse each surface's current brand position instead of recreating the Aquarium background. | SOURCE (project code) |

## Component Tree

| Component | Parent | Visual role | Implementation candidate | Evidence |
|---|---|---|---|---|
| Organization/product lockup | Existing surface header or body shell | Primary identity | One semantic external anchor with two text spans | SOURCE + user confirmation |
| Organization name | Lockup | Strong first line | `arena-brand-name` | SOURCE + user confirmation |
| Product name | Lockup | Quiet uppercase second line | `arena-brand-product` | SOURCE + user confirmation |
| Keyboard focus | Lockup | Link affordance | `:focus-visible` outline using Arena cyan | SOURCE (project accessibility convention) |

## Text Inventory

| Text | Location | Confidence | Notes |
|---|---|---:|---|
| Angel Software Solutions | Lockup first line | SOURCE | User-confirmed replacement for the reference image's older organization name. |
| THE ARENA | Lockup second line | SOURCE | User-confirmed Arena product label, following the reference hierarchy. |

## Design Tokens

- Colors: Arena white `#ecf4ff`, muted blue-gray `#91a6bd`, and focus/hover cyan `#47d7ff`.
- Typography: Space Grotesk 700 for the organization; IBM Plex Mono 600 uppercase for the product.
- Spacing: 4px inter-line gap, 3px block and 4px inline link padding, minimum 44px link height on standard surfaces.
- Radius: 6px focus/interaction footprint.
- Borders: no resting border; 2px cyan keyboard outline with 4px offset.
- Shadows: existing Arena dark text shadow for legibility over the live canvas.
- Motion: 180ms color change; reduced to 0.01ms when reduced motion is requested.

## Assets And Icons

| Asset/Icon | Meaning | Available locally? | Evidence |
|---|---|---|---|
| Aquarium header screenshot | Hierarchy and density reference | Yes, copied to `screenshots/source.png` | SOURCE |
| Legacy gradient dot | Old Arena identity mark | Yes, in prior CSS only | SOURCE (project code); deliberately removed from visible lockups |

## States And Interactions

| Element | State/interaction | Evidence | Needs confirmation? |
|---|---|---|---|
| Brand lockup | Opens `https://angel-serv.com/` in a new tab | User-confirmed destination | No |
| Brand lockup | Cyan product line on hover | Existing Arena accent convention | No |
| Brand lockup | Visible cyan keyboard outline | Existing Arena focus convention | No |
| Dashboard lockup | Reserved standalone top space; scrolls with the document and is hidden when embedded | Existing `dashboard-embedded` mode + responsive QA | No |

## Interactive Control Inventory

| Element | Control type | Visible state | Required implementation | Hidden behavior known? | Evidence |
|---|---|---|---|---|---|
| Brand lockup | Semantic link | Resting two-line identity | Native `<a>` with accessible name, external destination, and opener isolation | Yes | User request + project code |

## Open Questions

- None material. The source image proves hierarchy and density; the user supplied the new text and destination, while the existing Arena code supplies fonts, colors, breakpoints, and placement.

## Implementation Notes

- The Aquarium background artwork is reference context, not an Arena asset requirement, so it is not copied into production CSS.
- Descriptive uses of “AI Battle Arena” remain in explanatory content and accessibility labels; only the visible property lockup and document titles change.
- One shared stylesheet is loaded after each surface's existing styles so prior single-line and dot-specific rules cannot truncate the two-line lockup.
