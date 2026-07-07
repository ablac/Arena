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
