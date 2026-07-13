# Arena Brand Lockup Component Map

| Surface | Existing host | Production markup | Shared style | Behavior |
|---|---|---|---|---|
| Public Arena | `.site-header` | `.site-brand.arena-brand` | `frontend/css/brand-lockup.css` | Opens Angel Software Solutions in a new tab while the live Arena session stays open. |
| Cosmetic Shop | `.shop-site-header` | `.site-brand.arena-brand` | `frontend/css/brand-lockup.css` | Keeps Shop navigation independent from the corporate link. |
| Dashboard | top-level body shell | `.arena-brand.dashboard-arena-brand` | `frontend/css/brand-lockup.css` | Top-left with reserved document space for standalone use; scrolls away instead of covering content and stays hidden in `dashboard-embedded` mode. |
| Mobile spectator | `#topbar` | `#tb-brand.arena-brand` | `frontend/css/brand-lockup.css` | Replaces the dot-only link with a compact, readable two-line identity. |

## Shared Anatomy

```html
<a
  class="arena-brand"
  href="https://angel-serv.com/"
  target="_blank"
  rel="noopener"
  aria-label="Angel Software Solutions home">
  <span class="arena-brand-name">Angel Software Solutions</span>
  <span class="arena-brand-product">THE ARENA</span>
</a>
```

Host-specific classes and IDs remain in place where existing CSS or JavaScript depends on them. The lockup has no JavaScript behavior and introduces no new library or image dependency.
