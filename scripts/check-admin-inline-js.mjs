import fs from 'node:fs';

const file = 'frontend/admin/index.html';
const html = fs.readFileSync(file, 'utf8');
const scripts = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)];

if (scripts.length !== 1) {
  throw new Error(`${file}: expected exactly one inline script, found ${scripts.length}`);
}

// Parsing the function body catches syntax errors without executing browser
// APIs or requiring a DOM implementation in CI.
new Function(scripts[0][1]);
console.log(`${file}: inline JavaScript parses`);
