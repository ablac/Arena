/**
 * Pure helpers for deciding how the updater invokes `docker compose`. Kept
 * dependency-free and separate from server.mjs so they can be unit-tested
 * without standing up the HTTP server or a Docker daemon.
 *
 * Why this exists: the updater sidecar runs `docker compose` from its OWN
 * working directory (the deploy dir mounted at DEPLOY_DIR), whose basename is
 * NOT the same as the project the live orchestrator container belongs to. When
 * those disagree, compose treats the running container as belonging to a
 * different project and -- because the service pins a global `container_name` --
 * tries to CREATE a second container with a name already in use, failing with
 * "Conflict. The container name is already in use". The whole update path had
 * never once succeeded because of this.
 *
 * The robust fix, which also makes the sidecar reusable for any project without
 * hand-syncing env vars: read the project name and compose files straight off
 * the live container's own Docker Compose labels and reuse them verbatim.
 */

import { basename } from "node:path";

/**
 * Turn the `com.docker.compose.project.config_files` label (a comma-separated
 * list of absolute HOST paths) into paths relative to the deploy directory, so
 * they resolve inside the sidecar (which sees that directory at its cwd, not at
 * the host path). Uses the `com.docker.compose.project.working_dir` label to
 * strip the host prefix when possible (handles compose files in subdirectories
 * correctly); falls back to the bare basename otherwise.
 *
 * @param {string | null | undefined} configFilesLabel
 * @param {string | null | undefined} workingDirLabel
 * @returns {string[]}
 */
export function relativeComposeFiles(configFilesLabel, workingDirLabel) {
  const workdir = typeof workingDirLabel === "string" ? workingDirLabel.trim().replace(/[/\\]+$/, "") : "";
  return String(configFilesLabel ?? "")
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
    .map((entry) => {
      if (workdir !== "" && (entry === workdir || entry.startsWith(`${workdir}/`) || entry.startsWith(`${workdir}\\`))) {
        return entry.slice(workdir.length + 1);
      }
      return basename(entry);
    });
}

/**
 * Build the leading `docker compose` arguments (the `-p <project>` and repeated
 * `-f <file>` flags) shared by the build and up steps. Values detected from the
 * live container win; the env-configured fallbacks are used only when nothing
 * is running yet to detect from (e.g. a first-ever deploy via the sidecar).
 *
 * The result always carries an explicit `-p` when a project name is known, so
 * the invocation never silently falls back to the cwd basename -- the exact bug
 * this module exists to prevent.
 *
 * @param {{
 *   detectedProject?: string | null,
 *   detectedFiles?: string[] | null,
 *   envProject?: string | null,
 *   envFiles?: string[]
 * }} input
 * @returns {string[]}
 */
export function buildComposeBaseArgs(input) {
  const project = firstNonEmpty(input.detectedProject, input.envProject);
  const detectedFiles = Array.isArray(input.detectedFiles) ? input.detectedFiles.filter(Boolean) : [];
  const envFiles = Array.isArray(input.envFiles) ? input.envFiles.filter(Boolean) : [];
  const files = detectedFiles.length > 0 ? detectedFiles : envFiles;

  return [
    ...(project ? ["-p", project] : []),
    ...files.flatMap((file) => ["-f", file])
  ];
}

/**
 * Build the one-shot migration command used between stopping the old app and
 * recreating it from the freshly built image. Credentials are passed through
 * the compose process environment and referenced by name (`-e KEY`), never
 * embedded in argv where process listings and updater logs could expose them.
 *
 * The migrator password is optional for deployments where the runtime and
 * owner roles intentionally share the same password: omitting it leaves the
 * service's ARENA_DB_PASSWORD from `.env` intact while only switching roles.
 * The runtime role is always carried separately so the migration command can
 * grant DML/table and sequence access after owner-run DDL.
 *
 * @param {{
 *   composeBaseArgs?: string[],
 *   service?: string,
 *   migratorUser?: string,
 *   migratorPassword?: string,
 *   runtimeUser?: string,
 *   containerName?: string
 * }} input
 * @returns {{args: string[], env: Record<string, string>}}
 */
export function buildMigrationInvocation(input) {
  const service = typeof input.service === "string" ? input.service.trim() : "";
  const migratorUser = typeof input.migratorUser === "string" ? input.migratorUser : "";
  const migratorPassword = typeof input.migratorPassword === "string" ? input.migratorPassword : "";
  const runtimeUser = typeof input.runtimeUser === "string" ? input.runtimeUser : "";
  const containerName = typeof input.containerName === "string" ? input.containerName.trim() : "";
  if (service === "") {
    throw new Error("COMPOSE_SERVICE is not configured");
  }
  if (migratorUser === "") {
    throw new Error("ARENA_DB_MIGRATOR_USER is not configured");
  }
  if (runtimeUser === "") {
    throw new Error("ARENA_RUNTIME_DB_USER is not configured");
  }
  if (containerName === "") {
    throw new Error("migration container name is not configured");
  }

  const forwardedKeys = ["ARENA_DB_USER"];
  const env = { ARENA_DB_USER: migratorUser };
  if (migratorPassword !== "") {
    forwardedKeys.push("ARENA_DB_PASSWORD");
    env.ARENA_DB_PASSWORD = migratorPassword;
  }
  forwardedKeys.push("ARENA_RUNTIME_DB_USER");
  env.ARENA_RUNTIME_DB_USER = runtimeUser;

  return {
    args: [
      "compose",
      ...(Array.isArray(input.composeBaseArgs) ? input.composeBaseArgs : []),
      "run",
      "--no-deps",
      "--name",
      containerName,
      ...forwardedKeys.flatMap((key) => ["-e", key]),
      service,
      "/arena-server",
      "migrate"
    ],
    env
  };
}

/**
 * Force-stop and remove a named one-shot migration container. Killing a timed
 * out `docker compose run` CLI does not stop its container, so recovery may
 * restart the old database writer only after this cleanup succeeds. The
 * container is intentionally retained until its exit state is inspected;
 * exact absence is the only cleanup error that is safe to ignore after a
 * separately confirmed result.
 *
 * @param {{
 *   execFile: (file: string, args: string[], options: object) => Promise<unknown>,
 *   containerName: string,
 *   options?: object
 * }} input
 * @returns {Promise<"removed" | "absent">}
 */
export async function forceRemoveMigrationContainer(input) {
  try {
    await input.execFile("docker", ["rm", "-f", input.containerName], input.options ?? {});
    return "removed";
  } catch (error) {
    if (isMissingDockerContainerError(error)) {
      return "absent";
    }
    throw error;
  }
}

/**
 * Resolve an error from the attached Compose CLI without guessing whether the
 * migration itself failed. The named container is intentionally retained by
 * `compose run`: an exited-zero container proves the migration completed and
 * lets the update continue, while a running/nonzero container is force-stopped
 * before the old writer may recover. An absent container means Compose failed
 * before creating it. Any unclassifiable Docker error is propagated so the old
 * writer remains stopped.
 *
 * @param {{
 *   execFile: (file: string, args: string[], options: object) => Promise<unknown>,
 *   containerName: string,
 *   options?: object
 * }} input
 * @returns {Promise<{migrationSucceeded: boolean, state: string}>}
 */
export async function reconcileMigrationContainerAfterRunError(input) {
  let result;
  try {
    result = await input.execFile(
      "docker",
      ["inspect", "--format", "{{.State.Status}}\t{{.State.ExitCode}}", input.containerName],
      input.options ?? {}
    );
  } catch (error) {
    if (isMissingDockerContainerError(error)) {
      return { migrationSucceeded: false, state: "absent" };
    }
    throw error;
  }

  const output = String(result?.stdout ?? "").trim();
  const [status = "", exitCodeText = ""] = output.split("\t");
  const exitCode = Number.parseInt(exitCodeText, 10);
  if (status === "" || !Number.isInteger(exitCode)) {
    throw new Error(`could not parse migration container state: ${output || "empty inspect output"}`);
  }

  await forceRemoveMigrationContainer(input);
  return {
    migrationSucceeded: status === "exited" && exitCode === 0,
    state: `${status}:${exitCode}`
  };
}

function isMissingDockerContainerError(error) {
  const details = [error?.message, error?.stderr, error?.stdout]
    .filter((value) => typeof value === "string")
    .join("\n")
    .toLowerCase();
  return details.includes("no such container") || details.includes("no such object");
}

/**
 * Parse the tab-separated `docker inspect --format` output the sidecar uses to
 * read a container's compose labels, into a normalized context object. Split
 * out so the parsing is unit-testable independently of running `docker`.
 *
 * @param {string} inspectStdout  "<project>\t<working_dir>\t<config_files>"
 * @returns {{ project: string | null, files: string[] }}
 */
export function parseInspectedComposeContext(inspectStdout) {
  const [project = "", workdir = "", configFiles = ""] = String(inspectStdout ?? "").trim().split("\t");
  return {
    project: project.trim() === "" ? null : project.trim(),
    files: relativeComposeFiles(configFiles, workdir)
  };
}

/**
 * Decide which `uid:gid` the post-sync ownership restore should chown code files
 * back to. An explicit `DEPLOY_OWNER` ("uid:gid") always wins. Otherwise use the
 * deploy directory's own current owner -- EXCEPT when that is root (uid 0),
 * which is the corrupted state a prior root-run process (or a failed update
 * whose recreate never completed) can leave behind. Chowning code to root there
 * would cement it and lock the non-root operator out of their own deploy
 * directory (exactly what happened on 2026-07-07). Return null in that case so
 * the caller SKIPS the chown and warns, rather than perpetuating root ownership.
 *
 * @param {{ envOwner?: string | null, statUid?: number, statGid?: number }} input
 * @returns {string | null}
 */
export function resolveDeployOwner(input) {
  const fromEnv = parseOwner(input.envOwner);
  if (fromEnv !== null) {
    return fromEnv;
  }
  if (typeof input.statUid === "number" && input.statUid > 0 && typeof input.statGid === "number" && input.statGid >= 0) {
    return `${input.statUid}:${input.statGid}`;
  }
  return null;
}

function parseOwner(value) {
  if (typeof value !== "string") {
    return null;
  }
  const match = /^(\d+):(\d+)$/.exec(value.trim());
  return match === null ? null : `${match[1]}:${match[2]}`;
}

function firstNonEmpty(...values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim() !== "") {
      return value.trim();
    }
  }
  return null;
}
