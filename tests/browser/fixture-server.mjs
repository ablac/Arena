import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { extname, join, normalize, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = fileURLToPath(new URL('.', import.meta.url));
const repoRoot = resolve(here, '..', '..');
const frontendRoot = resolve(repoRoot, 'frontend');
const securityHeaderSource = await readFile(
  join(repoRoot, 'go-arena', 'internal', 'api', 'security_headers.go'),
  'utf8',
);

function readContentSecurityPolicy(source) {
  const declaration = source.match(
    /const contentSecurityPolicy = "" \+([\s\S]*?)\r?\n\r?\n\/\/ maxRequestBodyBytes/,
  );
  if (!declaration) throw new Error('unable to read the production Content Security Policy');
  const fragments = [...declaration[1].matchAll(/"((?:\\.|[^"\\])*)"/g)]
    .map((match) => JSON.parse(`"${match[1]}"`));
  if (fragments.length === 0) throw new Error('production Content Security Policy is empty');
  return fragments.join('');
}

const contentSecurityPolicy = readContentSecurityPolicy(securityHeaderSource);
const fixturePort = Number(process.env.ARENA_BROWSER_PORT) || 42817;
const contentTypes = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.gif', 'image/gif'],
  ['.html', 'text/html; charset=utf-8'],
  ['.ico', 'image/x-icon'],
  ['.jpeg', 'image/jpeg'],
  ['.jpg', 'image/jpeg'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.png', 'image/png'],
  ['.svg', 'image/svg+xml'],
  ['.webp', 'image/webp'],
]);

function applyProductionHeaders(response) {
  response.setHeader('Content-Security-Policy', contentSecurityPolicy);
  response.setHeader('X-Content-Type-Options', 'nosniff');
  response.setHeader('X-Frame-Options', 'SAMEORIGIN');
  response.setHeader('Referrer-Policy', 'strict-origin-when-cross-origin');
}

function safeFrontendPath(pathname) {
  let relative = decodeURIComponent(pathname).replace(/^\/arena(?=\/|$)/, '');
  if (relative === '/' || relative === '') relative = '/index.html';
  const candidate = normalize(resolve(frontendRoot, `.${relative}`));
  if (candidate !== frontendRoot && !candidate.startsWith(`${frontendRoot}${sep}`)) return null;
  return candidate;
}

const server = createServer(async (request, response) => {
  applyProductionHeaders(response);
  const url = new URL(request.url || '/', 'http://127.0.0.1');
  if (url.pathname === '/__health') {
    response.writeHead(204);
    response.end();
    return;
  }
  if (url.pathname === '/favicon.ico') {
    response.writeHead(204);
    response.end();
    return;
  }

  let filePath = safeFrontendPath(url.pathname);
  if (!filePath) {
    response.writeHead(403);
    response.end('forbidden');
    return;
  }
  try {
    const info = await stat(filePath);
    if (info.isDirectory()) filePath = join(filePath, 'index.html');
    const body = await readFile(filePath);
    response.setHeader('Cache-Control', 'no-store');
    response.setHeader('Content-Type', contentTypes.get(extname(filePath).toLowerCase()) || 'application/octet-stream');
    response.writeHead(200);
    response.end(body);
  } catch {
    response.writeHead(404);
    response.end('not found');
  }
});

server.listen(fixturePort, '127.0.0.1', () => {
  process.stdout.write(`Arena browser fixture listening on http://127.0.0.1:${fixturePort}\n`);
});

const close = () => server.close(() => process.exit(0));
process.on('SIGINT', close);
process.on('SIGTERM', close);
