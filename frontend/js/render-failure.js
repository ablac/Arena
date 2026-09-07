'use strict';

/**
 * What the page does when the 3D engine cannot start at all.
 *
 * This is the worst outcome the spectator pages have, and until this module
 * existed it was also the quietest. Both entrypoints did this and only this:
 *
 *     } catch (err) {
 *       console.error('[App] Engine init failed:', err);
 *     }
 *
 * So the spectator got a black rectangle under a live HUD and a live kill feed,
 * with nothing to say whether the match, their network or their browser was at
 * fault. And because the throw is CAUGHT there, `window.onerror` never fired, so
 * client-errors.js never reported it either: installClientErrorReporting() only
 * auto-reports uncaught errors and unhandled rejections, and an awaited failure
 * inside a try/catch is neither.
 *
 * A total rendering outage therefore left no user-facing message and no
 * server-side trace, which is the whole reason "the arena does not render for
 * some people" had no cause attached to it.
 *
 * Shared because both entrypoints have the failure, and the mobile one is where
 * the weakest GPUs actually are.
 *
 * @module render-failure
 */

/**
 * Report a fatal engine-init failure to /client-errors.
 *
 * The context fields are the ones triage needs and cannot recover afterwards:
 * whether WebGL was forced, and what the browser claims to support. A report
 * saying "WebGL not supported" from a browser that advertises WebGL is a
 * different bug from one that does not.
 *
 * @param {*} err the error `init()` threw
 * @param {string} source which entrypoint failed
 */
export function reportEngineInitFailure(err, source = 'app.arenaEngine.init') {
  try {
    globalThis.__arenaReportError?.('engine-init', err, {
      source,
      forcedWebGL: new URLSearchParams(location.search).get('webgpu') === '0',
      webgpuAdvertised: Boolean(navigator.gpu),
      webglAdvertised: webglAdvertised(),
    });
  } catch {
    // Reporting a failure must never become a second failure.
  }
}

/** Does this browser claim WebGL at all, independent of what Babylon did with it? */
function webglAdvertised() {
  try {
    const probe = document.createElement('canvas');
    return Boolean(probe.getContext('webgl2') || probe.getContext('webgl'));
  } catch {
    return false;
  }
}

/**
 * Put something truthful in the dead canvas's place.
 *
 * Deliberately additive: the HUD, the kill feed and the telemetry panel are all
 * still live and still worth reading, so this covers the canvas only and leaves
 * the rest of the page alone.
 *
 * The retry is `?webgpu=0` because WebGL is the backend that can still work when
 * WebGPU cannot, and it is already the flag the renderer honours. It is withheld
 * when WebGL itself is what failed, or when the spectator is already in
 * compatibility mode: offering a retry that cannot work is worse than offering
 * none.
 */
export function showArenaRenderFallback(err) {
  try {
    const panel = document.getElementById('arena-render-fallback');
    if (!panel) return;
    const forcedWebGL = new URLSearchParams(location.search).get('webgpu') === '0';
    const message = String((err && err.message) || err || '');
    const retryUseless = forcedWebGL || /webgl/i.test(message);
    const retry = document.getElementById('arena-render-fallback-retry');
    if (retry) retry.hidden = retryUseless;
    const detail = document.getElementById('arena-render-fallback-detail');
    if (detail && retryUseless) {
      detail.textContent =
        'The match is still running and the live feed is up to date, but this '
        + 'browser cannot draw the 3D view.';
    }
    panel.hidden = false;
    document.body.classList.add('arena-render-failed');
  } catch {
    // A failure notice that throws would take the rest of the page with it.
  }
}
