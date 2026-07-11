import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  buildMigrationInvocation,
  forceRemoveMigrationContainer,
  reconcileMigrationContainerAfterRunError
} from "./compose.mjs";

test("migration invocation uses the freshly built server image without exposing credentials in argv", () => {
  const invocation = buildMigrationInvocation({
    composeBaseArgs: ["-p", "arena", "-f", "docker-compose.yml"],
    service: "arena-server",
    migratorUser: "arena_owner",
    migratorPassword: "owner-password",
    runtimeUser: "arena_app",
    containerName: "arena-migration-test"
  });

  assert.deepEqual(invocation.args, [
    "compose",
    "-p",
    "arena",
    "-f",
    "docker-compose.yml",
    "run",
    "--no-deps",
    "--name",
    "arena-migration-test",
    "-e",
    "ARENA_DB_USER",
    "-e",
    "ARENA_DB_PASSWORD",
    "-e",
    "ARENA_RUNTIME_DB_USER",
    "arena-server",
    "/arena-server",
    "migrate"
  ]);
  assert.deepEqual(invocation.env, {
    ARENA_DB_USER: "arena_owner",
    ARENA_DB_PASSWORD: "owner-password",
    ARENA_RUNTIME_DB_USER: "arena_app"
  });
  assert.equal(invocation.args.includes("arena_owner"), false);
  assert.equal(invocation.args.includes("owner-password"), false);
  assert.equal(invocation.args.includes("arena_app"), false);
});

test("migration invocation can reuse the service password while still switching roles", () => {
  const invocation = buildMigrationInvocation({
    composeBaseArgs: [],
    service: "arena-server",
    migratorUser: "arena_owner",
    migratorPassword: "",
    runtimeUser: "arena_app",
    containerName: "arena-migration-test"
  });

  assert.deepEqual(invocation.args, [
    "compose",
    "run",
    "--no-deps",
    "--name",
    "arena-migration-test",
    "-e",
    "ARENA_DB_USER",
    "-e",
    "ARENA_RUNTIME_DB_USER",
    "arena-server",
    "/arena-server",
    "migrate"
  ]);
  assert.deepEqual(invocation.env, {
    ARENA_DB_USER: "arena_owner",
    ARENA_RUNTIME_DB_USER: "arena_app"
  });
});

test("migration invocation fails closed when dedicated role identities are absent", () => {
  assert.throws(
    () =>
      buildMigrationInvocation({
        composeBaseArgs: [],
        service: "arena-server",
        migratorUser: "",
        migratorPassword: "secret",
        runtimeUser: "arena_app",
        containerName: "arena-migration-test"
      }),
    /ARENA_DB_MIGRATOR_USER/
  );
  assert.throws(
    () =>
      buildMigrationInvocation({
        composeBaseArgs: [],
        service: "arena-server",
        migratorUser: "arena_owner",
        migratorPassword: "",
        runtimeUser: "",
        containerName: "arena-migration-test"
      }),
    /ARENA_RUNTIME_DB_USER/
  );
  assert.throws(
    () =>
      buildMigrationInvocation({
        composeBaseArgs: [],
        service: "arena-server",
        migratorUser: "arena_owner",
        migratorPassword: "",
        runtimeUser: "arena_app",
        containerName: ""
      }),
    /migration container name/
  );
});

test("migration cleanup force-removes a timed-out container and only tolerates confirmed absence", async () => {
  const calls = [];
  const removed = await forceRemoveMigrationContainer({
    execFile: async (...args) => calls.push(args),
    containerName: "arena-migration-test",
    options: { timeout: 1000 }
  });
  assert.equal(removed, "removed");
  assert.deepEqual(calls, [["docker", ["rm", "-f", "arena-migration-test"], { timeout: 1000 }]]);

  const absent = await forceRemoveMigrationContainer({
    execFile: async () => {
      const error = new Error("docker rm failed");
      error.stderr = "Error response from daemon: No such container: arena-migration-test";
      throw error;
    },
    containerName: "arena-migration-test"
  });
  assert.equal(absent, "absent");

  await assert.rejects(
    forceRemoveMigrationContainer({
      execFile: async () => {
        throw new Error("cannot reach Docker daemon");
      },
      containerName: "arena-migration-test"
    }),
    /cannot reach Docker daemon/
  );
});

test("migration reconciliation distinguishes completed, failed, running, and absent containers", async () => {
  for (const scenario of [
    { inspect: "exited\t0\n", succeeded: true, state: "exited:0" },
    { inspect: "exited\t1\n", succeeded: false, state: "exited:1" },
    { inspect: "running\t0\n", succeeded: false, state: "running:0" }
  ]) {
    const calls = [];
    const result = await reconcileMigrationContainerAfterRunError({
      execFile: async (_file, args) => {
        calls.push(args);
        return args[0] === "inspect" ? { stdout: scenario.inspect } : { stdout: "" };
      },
      containerName: "arena-migration-test"
    });
    assert.deepEqual(result, { migrationSucceeded: scenario.succeeded, state: scenario.state });
    assert.deepEqual(calls[1], ["rm", "-f", "arena-migration-test"]);
  }

  for (const missingMessage of [
    "Error response from daemon: No such container: arena-migration-test",
    "Error: No such object: arena-migration-test",
    "error: no such object: arena-migration-test"
  ]) {
    const absent = await reconcileMigrationContainerAfterRunError({
      execFile: async () => {
        const error = new Error("inspect failed");
        error.stderr = missingMessage;
        throw error;
      },
      containerName: "arena-migration-test"
    });
    assert.deepEqual(absent, { migrationSucceeded: false, state: "absent" });
  }

  await assert.rejects(
    reconcileMigrationContainerAfterRunError({
      execFile: async () => {
        throw new Error("cannot reach Docker daemon");
      },
      containerName: "arena-migration-test"
    }),
    /cannot reach Docker daemon/
  );
});

test("updater quiesces the old writer before migrating and can recover it on failure", async () => {
  const source = await readFile(new URL("./server.mjs", import.meta.url), "utf8");
  const build = source.indexOf('onPhase("building")');
  const drain = source.indexOf('onPhase("draining")');
  const stop = source.indexOf('onPhase("stopping")');
  const migrate = source.indexOf('onPhase("migrating")');
  const recover = source.indexOf('onPhase("recovering")');
  const reconcile = source.indexOf("await reconcileMigrationContainerAfterRunError");
  const cleanup = source.indexOf("await forceRemoveMigrationContainer");
  const restartOld = source.indexOf('"start", COMPOSE_SERVICE');
  const recreate = source.indexOf('onPhase("recreating")');

  assert.ok(build >= 0, "build phase is present");
  assert.ok(drain > build, "draining follows a successful image build");
  assert.ok(stop > drain, "the old writer stops after receiving the drain notice");
  assert.ok(migrate > stop, "migration starts only after the old writer is stopped");
  assert.ok(recover > migrate, "the failed-migration path restarts the stopped app");
  assert.ok(reconcile > migrate, "a Compose-client error is reconciled against the retained container state");
  assert.ok(cleanup > reconcile, "a successful migration container is explicitly removed");
  assert.ok(restartOld > cleanup, "the old writer restarts only after migration cleanup is confirmed");
  assert.ok(recreate > recover, "new-image recreation only follows a successful migration");
  assert.match(source, /so the previous app was not restarted/,
    "cleanup failure keeps the previous writer stopped");
  assert.match(source, /"start", COMPOSE_SERVICE/, "recovery starts the retained old container");
});

test("deployment guide calls out the one-time updater self-rebuild", async () => {
  const guide = await readFile(new URL("../docs/build-and-deploy.md", import.meta.url), "utf8");
  assert.match(guide, /set -a\s+\. \.\/\.env\s+set \+a/);
  assert.match(guide, /Existing updater installations must be rebuilt and recreated once/);
  assert.match(guide, /docker compose --profile updater build arena-updater/);
  assert.match(guide, /docker compose --profile updater up -d --no-deps arena-updater/);
});
