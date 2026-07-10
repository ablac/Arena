'use strict';

const MOUNT_PATH = '/arena';

export function mountPrefix(pathname = window.location.pathname) {
  const path = pathname || '/';
  return path === MOUNT_PATH || path.startsWith(`${MOUNT_PATH}/`) ? MOUNT_PATH : '';
}

function withLeadingSlash(path) {
  if (!path) return '/';
  return path.startsWith('/') ? path : `/${path}`;
}

export function appPath(path = '/', pathname = window.location.pathname) {
  return `${mountPrefix(pathname)}${withLeadingSlash(path)}`;
}

export function apiBase(pathname = window.location.pathname) {
  return `${mountPrefix(pathname)}/api/v1`;
}

export function apiPath(path = '', pathname = window.location.pathname) {
  if (!path) return apiBase(pathname);
  return `${apiBase(pathname)}${withLeadingSlash(path)}`;
}

export function wsURL(path, locationLike = window.location) {
  const protocol = locationLike.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${locationLike.host}${mountPrefix(locationLike.pathname)}/ws${withLeadingSlash(path)}`;
}

// Compatibility surface for the dashboard's existing classic inline script.
// Module consumers should use the named exports above.
if (typeof window !== 'undefined') {
  window.ArenaPaths = Object.freeze({ mountPrefix, appPath, apiBase, apiPath, wsURL });
}
