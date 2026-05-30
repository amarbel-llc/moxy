#!/usr/bin/env zx

// Launch the list-changed POC MCP server (zz-pocs/list-changed/serve.ts) on
// an OS-assigned ephemeral port via the clown-plugin-protocol handshake,
// generate `.tmp/.mcp.json` pointing at it, and drop into the user's $SHELL.
//
// Usage:
//   zx bin/serve-poc-list-changed.mjs
//   just serve-poc-list-changed
//
// Test loop:
//   1. Inside the spawned shell: `claude --mcp-config $MOXY_DEV_MCP_JSON`
//   2. Confirm the `alpha-only` tool is visible.
//   3. Call the `flip` tool. The POC swaps to beta and broadcasts
//      `notifications/tools/list_changed`.
//   4. End the turn. Start a new user prompt.
//   5. Ask Claude to call `beta-only`. If it succeeds → tool registry
//      refreshed across turns. If it fails because the tool isn't visible
//      → Claude Code did not refresh.
//
// Lifecycle: when the spawned shell exits, the POC is killed and the
// generated .mcp.json is removed.

import { spawn } from "node:child_process";
import { openSync } from "node:fs";

$.verbose = false;

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname);
const REPO_ROOT = path.resolve(SCRIPT_DIR, "..");
const POC_ENTRYPOINT = path.join(
  REPO_ROOT,
  "zz-pocs",
  "list-changed",
  "serve.ts",
);
const TMP_DIR = path.join(REPO_ROOT, ".tmp");
const MCP_JSON = path.join(TMP_DIR, ".mcp.json");
const LOG_FILE = path.join(REPO_ROOT, "build", "serve-poc-list-changed.log");

await fs.ensureDir(TMP_DIR);
await fs.ensureDir(path.dirname(LOG_FILE));

const logFd = openSync(LOG_FILE, "w");

const poc = spawn("bun", [POC_ENTRYPOINT], {
  stdio: ["ignore", "pipe", logFd],
  env: process.env,
});

process.on("exit", () => {
  try {
    poc.kill();
  } catch {}
  try {
    fs.removeSync(MCP_JSON);
  } catch {}
});

// Parse the first stdout line: "1|1|tcp|<host:port>|streamable-http".
const addr = await new Promise((resolve, reject) => {
  let buf = "";
  const timer = setTimeout(() => {
    poc.stdout.off("data", onData);
    poc.off("exit", onExit);
    reject(new Error(`POC handshake timeout; see ${LOG_FILE}`));
  }, 10_000);
  const settle = (fn, value) => {
    clearTimeout(timer);
    poc.stdout.off("data", onData);
    poc.off("exit", onExit);
    fn(value);
  };
  const onData = (chunk) => {
    buf += chunk.toString("utf8");
    const nl = buf.indexOf("\n");
    if (nl < 0) return;
    const line = buf.slice(0, nl);
    const parts = line.split("|");
    if (parts.length < 5 || parts[0] !== "1" || parts[2] !== "tcp") {
      settle(reject, new Error(`unexpected handshake line: ${line}`));
      return;
    }
    settle(resolve, parts[3]);
  };
  const onExit = (code) =>
    settle(
      reject,
      new Error(`POC exited before handshake (code ${code}); see ${LOG_FILE}`),
    );
  poc.stdout.on("data", onData);
  poc.on("exit", onExit);
});

// /healthz readiness gate.
const healthURL = `http://${addr}/healthz`;
let ready = false;
for (let i = 0; i < 60; i++) {
  try {
    const resp = await fetch(healthURL);
    if (resp.ok) {
      ready = true;
      break;
    }
  } catch {
    // not ready yet
  }
  await sleep(100);
}
if (!ready) {
  console.error(
    `POC /healthz never became 200 at ${healthURL}; see ${LOG_FILE}`,
  );
  poc.kill();
  process.exit(1);
}

const mcpURL = `http://${addr}/mcp`;
const mcpConfig = {
  mcpServers: {
    "list-changed-poc": { type: "http", url: mcpURL },
  },
};
await fs.writeFile(MCP_JSON, JSON.stringify(mcpConfig, null, 2) + "\n");

console.log("");
console.log(`POC listening on ${addr}`);
console.log(`  log:        ${LOG_FILE}`);
console.log(`  .mcp.json:  ${MCP_JSON}`);
console.log("");
console.log("Launch Claude Code pointed at this config:");
console.log(`  claude --mcp-config ${MCP_JSON}`);
console.log("");
console.log("Test loop:");
console.log('  1. In Claude, call the `state` tool. Expect "alpha".');
console.log('  2. Call the `flip` tool. Expect "alpha -> beta".');
console.log("  3. **End the turn.** Start a new user prompt.");
console.log("  4. Ask Claude to call `beta-only`.");
console.log("     - If it succeeds → tool registry refreshed.");
console.log(
  "     - If Claude says it can\u2019t find the tool → registry stale.",
);
console.log("");
console.log("Exit the shell to stop the POC and remove .mcp.json.");
console.log("");

const shell = process.env.SHELL ?? "/bin/bash";
const shellChild = spawn(shell, [], {
  stdio: "inherit",
  cwd: REPO_ROOT,
  env: { ...process.env, MOXY_DEV_MCP_JSON: MCP_JSON },
});

const exitCode = await new Promise((resolve) => {
  shellChild.on("exit", (code) => resolve(code ?? 0));
  shellChild.on("error", () => resolve(1));
});

poc.kill();
try {
  await fs.remove(MCP_JSON);
} catch {}

process.exit(exitCode);
