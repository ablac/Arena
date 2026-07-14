import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  MANIFEST_FILENAME,
  listReleaseFiles,
  isSafeManifestPath,
  vanishedFiles,
  readManifest,
  writeManifest,
  removeVanishedFiles
} from "./manifest.mjs";

const EXCLUDED = ["/.git", "/.env"];

async function makeTree(spec) {
  const root = await mkdtemp(join(tmpdir(), "manifest-test-"));
  for (const [path, content] of Object.entries(spec)) {
    const absolute = join(root, ...path.split("/"));
    await mkdir(join(absolute, ".."), { recursive: true });
    await writeFile(absolute, content);
  }
  return root;
}

test("listReleaseFiles returns sorted slash-separated relative paths", async () => {
  const root = await makeTree({
    "go-arena/internal/demobots/ai.go": "package demobots",
    "frontend/js/app.js": "app",
    "README.md": "readme"
  });
  assert.deepEqual(await listReleaseFiles(root), [
    "README.md",
    "frontend/js/app.js",
    "go-arena/internal/demobots/ai.go"
  ]);
});

test("vanished files are exactly the repo-owned deletions", () => {
  const previous = [
    "go-arena/internal/demobots/ai.go",
    "go-arena/internal/demobots/bot.go",
    "frontend/js/app.js"
  ];
  const current = [
    "go-arena/internal/demobots/bot.go",
    "go-arena/internal/demobots/routing.go",
    "frontend/js/app.js"
  ];
  assert.deepEqual(vanishedFiles(previous, current, EXCLUDED), [
    "go-arena/internal/demobots/ai.go"
  ]);
});

test("manifest paths cannot escape the deploy dir or reach excluded trees", () => {
  assert.equal(isSafeManifestPath("go-arena/main.go", EXCLUDED), true);
  assert.equal(isSafeManifestPath("../outside", EXCLUDED), false);
  assert.equal(isSafeManifestPath("a/../../outside", EXCLUDED), false);
  assert.equal(isSafeManifestPath("/etc/passwd", EXCLUDED), false);
  assert.equal(isSafeManifestPath("C:/windows/system32", EXCLUDED), false);
  assert.equal(isSafeManifestPath("a\\b", EXCLUDED), false);
  assert.equal(isSafeManifestPath(".git/config", EXCLUDED), false);
  assert.equal(isSafeManifestPath(".env", EXCLUDED), false);
  assert.equal(isSafeManifestPath("", EXCLUDED), false);
});

test("removeVanishedFiles deletes upstream-removed files and nothing else", async () => {
  const deploy = await makeTree({
    "go-arena/internal/demobots/ai.go": "stale",
    "go-arena/internal/demobots/routing.go": "new",
    ".env": "SECRET=1",
    "docker-compose.override.yml": "operator-only"
  });
  const previous = [
    "go-arena/internal/demobots/ai.go",
    "go-arena/internal/demobots/routing.go"
  ];
  const current = ["go-arena/internal/demobots/routing.go"];

  const removed = await removeVanishedFiles(deploy, previous, current, EXCLUDED);
  assert.deepEqual(removed, ["go-arena/internal/demobots/ai.go"]);

  await assert.rejects(stat(join(deploy, "go-arena/internal/demobots/ai.go")));
  assert.equal(await readFile(join(deploy, ".env"), "utf8"), "SECRET=1");
  assert.equal(
    await readFile(join(deploy, "docker-compose.override.yml"), "utf8"),
    "operator-only"
  );
});

test("removeVanishedFiles tolerates already-missing files", async () => {
  const deploy = await makeTree({ "keep.txt": "keep" });
  const removed = await removeVanishedFiles(deploy, ["gone.txt", "keep.txt"], ["keep.txt"], EXCLUDED);
  assert.deepEqual(removed, []);
});

test("manifest round-trips and tolerates corruption", async () => {
  const deploy = await makeTree({});
  assert.equal(await readManifest(deploy), null);

  await writeManifest(deploy, "abc123", ["a.txt", "b/c.txt"]);
  assert.deepEqual(await readManifest(deploy), ["a.txt", "b/c.txt"]);

  await writeFile(join(deploy, MANIFEST_FILENAME), "{not json");
  assert.equal(await readManifest(deploy), null);
});
