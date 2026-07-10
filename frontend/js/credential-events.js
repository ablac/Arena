'use strict';

export const CLEAR_ARENA_API_KEY_EVENT = 'arena:clear-api-key';

/**
 * Track which asynchronous UI request is allowed to update shared state.
 * Starting a newer request invalidates every older response.
 */
export function createLatestRequestGate() {
  let current = 0;
  return Object.freeze({
    next() {
      current += 1;
      return current;
    },
    isCurrent(version) {
      return version === current;
    },
    invalidate() {
      current += 1;
    },
  });
}

/** Notify every credential-bearing component to zero its in-memory/DOM key. */
export function requestArenaAPIKeyClear(target = window) {
  target.dispatchEvent(new Event(CLEAR_ARENA_API_KEY_EVENT));
}

/** Subscribe to the page-wide API-key clear request. */
export function onArenaAPIKeyClear(listener, target = window) {
  target.addEventListener(CLEAR_ARENA_API_KEY_EVENT, listener);
  return () => target.removeEventListener(CLEAR_ARENA_API_KEY_EVENT, listener);
}
