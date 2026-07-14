import { test } from "node:test";
import assert from "node:assert/strict";
import {
  buildGitSSHCommand,
  demobotsConfigFromEnv,
  demobotsState,
  deployKeyPath,
  isFullSha,
  parseLsRemote
} from "./demobots.mjs";

test("parseLsRemote extracts the SHA from ls-remote output", () => {
  const sha = "0123456789abcdef0123456789abcdef01234567";
  assert.equal(parseLsRemote(`${sha}\trefs/heads/main\n`), sha);
});

test("parseLsRemote rejects empty and malformed output", () => {
  assert.equal(parseLsRemote(""), null);
  assert.equal(parseLsRemote("\n"), null);
  assert.equal(parseLsRemote("not-a-sha\trefs/heads/main\n"), null);
  assert.equal(parseLsRemote("0123456\trefs/heads/main\n"), null);
});

test("isFullSha accepts only full lowercase hex SHAs", () => {
  assert.equal(isFullSha("0123456789abcdef0123456789abcdef01234567"), true);
  assert.equal(isFullSha("0123456789ABCDEF0123456789ABCDEF01234567"), false);
  assert.equal(isFullSha("0123456"), false);
  assert.equal(isFullSha(null), false);
});

test("demobotsConfigFromEnv applies defaults and env overrides", () => {
  const defaults = demobotsConfigFromEnv({});
  assert.equal(defaults.gitUrl, "");
  assert.equal(defaults.branch, "main");
  assert.equal(defaults.deployDir, "/demobots-deploy");
  assert.equal(defaults.image, "arena-demobots:local");
  assert.equal(defaults.composeService, "arena-demobots");
  assert.equal(defaults.composeProject, "arena-demobots");

  const custom = demobotsConfigFromEnv({
    DEMOBOTS_GIT_URL: "git@github.com:ablac/arena-demobots.git",
    DEMOBOTS_BRANCH: "release",
    DEMOBOTS_DEPLOY_DIR: "/elsewhere"
  });
  assert.equal(custom.gitUrl, "git@github.com:ablac/arena-demobots.git");
  assert.equal(custom.branch, "release");
  assert.equal(custom.deployDir, "/elsewhere");
});

test("deployKeyPath lives inside the fleet deploy dir", () => {
  const config = demobotsConfigFromEnv({ DEMOBOTS_DEPLOY_DIR: "/demobots-deploy" });
  assert.equal(deployKeyPath(config), "/demobots-deploy/.deploy-key");
});

test("buildGitSSHCommand pins the key and non-interactive host key policy", () => {
  const cmd = buildGitSSHCommand("/demobots-deploy/.deploy-key");
  assert.match(cmd, /^ssh -i \/demobots-deploy\/\.deploy-key /);
  assert.match(cmd, /IdentitiesOnly=yes/);
  assert.match(cmd, /StrictHostKeyChecking=accept-new/);
});

test("demobotsState reports configured from the git URL", () => {
  const off = demobotsState(demobotsConfigFromEnv({}));
  assert.equal(off.configured, false);
  assert.equal(off.inProgress, false);
  const on = demobotsState(demobotsConfigFromEnv({ DEMOBOTS_GIT_URL: "git@github.com:a/b.git" }));
  assert.equal(on.configured, true);
});
