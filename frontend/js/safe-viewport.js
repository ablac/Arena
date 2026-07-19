'use strict';

const DEFAULT_PADDING = 12;

function intersectRect(a, b) {
  const left = Math.max(a.left, b.left);
  const top = Math.max(a.top, b.top);
  const right = Math.min(a.right, b.right);
  const bottom = Math.min(a.bottom, b.bottom);
  if (right <= left || bottom <= top) return null;
  return { left, top, right, bottom };
}

function fitInsets(first, second, available) {
  const total = first + second;
  if (total <= available || total <= 0) return [first, second];
  const scale = Math.max(0, available) / total;
  return [first * scale, second * scale];
}

/**
 * Calculate the unobscured canvas region from overlay rectangles. All output
 * coordinates are local to the canvas. Insets are clamped so a large open
 * drawer can never push the camera completely out of frame.
 */
export function computeSafeViewport(canvasRect, regions, padding = DEFAULT_PADDING) {
  const canvasWidth = canvasRect.width ?? (canvasRect.right - canvasRect.left);
  const canvasHeight = canvasRect.height ?? (canvasRect.bottom - canvasRect.top);
  const requested = { left: 0, top: 0, right: 0, bottom: 0 };

  for (const region of regions || []) {
    if (!region?.rect || region.visible === false || !(region.side in requested)) continue;
    const intersection = intersectRect(canvasRect, region.rect);
    if (!intersection) continue;
    let inset = 0;
    if (region.side === 'left') inset = intersection.right - canvasRect.left;
    if (region.side === 'right') inset = canvasRect.right - intersection.left;
    if (region.side === 'top') inset = intersection.bottom - canvasRect.top;
    if (region.side === 'bottom') inset = canvasRect.bottom - intersection.top;
    requested[region.side] = Math.max(requested[region.side], inset + padding);
  }

  const minimumWidth = Math.max(140, canvasWidth * 0.35);
  const minimumHeight = Math.max(180, canvasHeight * 0.38);
  const [left, right] = fitInsets(requested.left, requested.right, canvasWidth - minimumWidth);
  const [top, bottom] = fitInsets(requested.top, requested.bottom, canvasHeight - minimumHeight);
  const width = Math.max(0, canvasWidth - left - right);
  const height = Math.max(0, canvasHeight - top - bottom);
  const centerX = left + width / 2;
  const centerY = top + height / 2;

  return {
    left, top, right, bottom, width, height, centerX, centerY,
    canvasWidth, canvasHeight,
    focalOffsetX: centerX - canvasWidth / 2,
    focalOffsetY: centerY - canvasHeight / 2,
  };
}

const DESKTOP_REGIONS = [
  { side: 'top', selector: '.site-header' },
  { side: 'top', selector: '.arena-shell-head' },
  { side: 'left', selector: '#hud-round' },
  { side: 'left', selector: '.arena-sidebar' },
  { side: 'right', selector: '.site-command-dock' },
  { side: 'right', selector: '.arena-container > canvas#arena-canvas ~ canvas' },
  { side: 'bottom', selector: '.arena-controls' },
];

function isVisible(element, view) {
  if (!element || element.hidden || element.getClientRects().length === 0) return false;
  const style = view.getComputedStyle(element);
  return style.display !== 'none' && style.visibility !== 'hidden' && Number(style.opacity || 1) > 0;
}

/**
 * Observe UI geometry at change boundaries, then publish a safe viewport.
 * No layout reads happen in the Babylon render loop.
 */
export function observeArenaSafeViewport(canvas, onChange, regionSpecs = DESKTOP_REGIONS) {
  const doc = canvas.ownerDocument;
  const view = doc.defaultView;
  const elements = regionSpecs.flatMap((spec) =>
    [...doc.querySelectorAll(spec.selector)].map((element) => ({ ...spec, element })),
  );
  let frame = 0;

  const measure = () => {
    frame = 0;
    const canvasRect = canvas.getBoundingClientRect();
    if (canvasRect.width <= 0 || canvasRect.height <= 0) return;
    const regions = elements.map(({ side, element }) => ({
      side,
      visible: isVisible(element, view),
      rect: element.getBoundingClientRect(),
    }));
    onChange(computeSafeViewport(canvasRect, regions));
  };
  const schedule = () => {
    if (!frame) frame = view.requestAnimationFrame(measure);
  };

  const resizeObserver = typeof view.ResizeObserver === 'function'
    ? new view.ResizeObserver(schedule)
    : null;
  resizeObserver?.observe(canvas);
  for (const { element } of elements) resizeObserver?.observe(element);

  const mutationObserver = typeof view.MutationObserver === 'function'
    ? new view.MutationObserver(schedule)
    : null;
  for (const { element } of elements) {
    mutationObserver?.observe(element, {
      attributes: true,
      attributeFilter: ['class', 'style', 'hidden', 'aria-expanded'],
    });
  }
  view.addEventListener('resize', schedule);
  view.addEventListener('orientationchange', schedule);
  schedule();

  return () => {
    if (frame) view.cancelAnimationFrame(frame);
    resizeObserver?.disconnect();
    mutationObserver?.disconnect();
    view.removeEventListener('resize', schedule);
    view.removeEventListener('orientationchange', schedule);
  };
}

export const MOBILE_SAFE_VIEWPORT_REGIONS = [
  { side: 'top', selector: '#topbar' },
  { side: 'right', selector: '#fabs' },
  { side: 'right', selector: '#minimap-box' },
  { side: 'bottom', selector: '#sheet' },
];
