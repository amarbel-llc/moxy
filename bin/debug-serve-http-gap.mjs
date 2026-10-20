#!/usr/bin/env zx

// Measure the connection-refused ("502 gap") window of `moxy serve-http
// --listen` after a (re)start, with a child that is slow to initialize.
//
// Usage: zx bin/debug-serve-http-gap.mjs [moxy-bin] [child-delay-seconds]
//
// Reports, in seconds from process spawn:
//   tcp_accept   first moment a TCP connection to the bound port succeeds
//                (before it, a tunnel in front sees ECONNREFUSED -> 502)
//   healthz      first HTTP answer on /healthz (any status)
//   initialize   first well-formed 200 answer to an MCP initialize
//   refused_gap  == tcp_accept: how long the origin refused connections
//
// Serves the dev loop for moxy#<serve-http-listen-early>; see
// moxy-serve-http(1) "STARTUP".

import net from "node:net";
import os from "node:os";
import { spawn, execFileSync } from "node:child_process";

$.verbose = false;

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname);
const REPO_ROOT = path.resolve(SCRIPT_DIR, "..");
const moxyBin = path.resolve(
  String(argv._[0] ?? path.join(REPO_ROOT, "build", "moxy")),
);
const delay = Number(argv._[1] ?? 8);

const home = await fs.mkdtemp(path.join(os.tmpdir(), "moxy-gap-"));
const repo = path.join(home, "repo");
await fs.ensureDir(repo);
await fs.writeFile(
  path.join(repo, "moxyfile"),
  `[[servers]]\nname = "slow"\ncommand = ["bash", "${path.join(REPO_ROOT, "zz-tests_bats/test-fixtures/slow-start-server.bash")}"]\n`,
);
const emptyMoxins = path.join(home, "moxins");
await fs.ensureDir(emptyMoxins);

execFileSync(process.env.MADDER_BIN ?? "madder", ["init", ".default"], {
  cwd: home,
  env: { ...process.env, HOME: home },
  stdio: "ignore",
});

const port = await new Promise((resolve) => {
  const s = net.createServer();
  s.listen(0, "127.0.0.1", () => {
    const p = s.address().port;
    s.close(() => resolve(p));
  });
});

const t0 = performance.now();
const since = () => ((performance.now() - t0) / 1000).toFixed(2);
const moxy = spawn(moxyBin, ["serve-http", "--listen", `127.0.0.1:${port}`], {
  cwd: repo,
  stdio: ["ignore", "ignore", "ignore"],
  env: {
    ...process.env,
    HOME: home,
    MOXIN_PATH: emptyMoxins,
    MOXY_HEARTBEAT_INTERVAL: "0",
    SLOW_START_SECONDS: String(delay),
  },
});
process.on("exit", () => {
  try {
    moxy.kill();
  } catch {}
});

const result = { tcp_accept: null, healthz: null, initialize: null };
const initBody = JSON.stringify({
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: {
    protocolVersion: "2025-11-25",
    capabilities: {},
    clientInfo: { name: "gap-probe", version: "0" },
  },
});

const tcpProbe = () =>
  new Promise((resolve) => {
    const c = net.connect(port, "127.0.0.1");
    c.once("connect", () => (c.destroy(), resolve(true)));
    c.once("error", () => resolve(false));
  });

const deadline = performance.now() + (delay + 60) * 1000;
const tcpLoop = (async () => {
  while (result.tcp_accept === null && performance.now() < deadline) {
    if (await tcpProbe()) result.tcp_accept = since();
    else await sleep(50);
  }
})();
const healthLoop = (async () => {
  while (result.healthz === null && performance.now() < deadline) {
    try {
      await fetch(`http://127.0.0.1:${port}/healthz`, {
        signal: AbortSignal.timeout(2000),
      });
      result.healthz = since();
    } catch {
      await sleep(50);
    }
  }
})();
const initLoop = (async () => {
  while (result.initialize === null && performance.now() < deadline) {
    try {
      const resp = await fetch(`http://127.0.0.1:${port}/mcp`, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          accept: "application/json, text/event-stream",
        },
        body: initBody,
        signal: AbortSignal.timeout((delay + 30) * 1000),
      });
      const json = await resp.json().catch(() => null);
      if (resp.status === 200 && json?.result?.serverInfo) {
        result.initialize = since();
      } else {
        await sleep(50);
      }
    } catch {
      await sleep(50);
    }
  }
})();

await Promise.all([tcpLoop, healthLoop, initLoop]);
moxy.kill();
await fs.remove(home);

console.log(
  JSON.stringify(
    { child_delay_s: delay, ...result, refused_gap: result.tcp_accept },
    null,
    2,
  ),
);
