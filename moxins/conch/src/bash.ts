// conch.bash — run a bash command inside a fence(1) sandbox with strict policy.
//
// Policy is generated per call into a tempfile, splicing the live $PWD into
// the allowed read/write paths. The wrapped command runs via:
//
//   fence --settings <tmp.jsonc> -- bash -c "<command> 2>&1"
//
// stdout+stderr are captured as one combined stream (interleaved by the
// inner bash via `2>&1`). On timeout or nonzero exit we append a marker
// line and return isError=true. On success we return the bare output.
//
// Output schema (result-type = mcp-result):
//   { "content": [{"type":"text","text":"<combined>"}], "isError": <bool> }

import { $ } from "zx";
import * as fsp from "node:fs/promises";
import * as nodePath from "node:path";
import * as os from "node:os";

$.verbose = false;

const TIMEOUT_DEFAULT = 60;
const TIMEOUT_MAX = 600;

// ---- args ------------------------------------------------------------------

const [command, timeoutArg] = process.argv.slice(2);

if (!command) {
  emit("ERROR: command is required", true);
  process.exit(0);
}

let timeoutSeconds = TIMEOUT_DEFAULT;
if (timeoutArg && timeoutArg.length > 0) {
  const n = Number.parseInt(timeoutArg, 10);
  if (!Number.isFinite(n) || n <= 0) {
    emit(`ERROR: timeout_seconds must be a positive integer, got ${timeoutArg}`, true);
    process.exit(0);
  }
  timeoutSeconds = Math.min(n, TIMEOUT_MAX);
}

// ---- fence policy ----------------------------------------------------------

const cwd = process.cwd();
const sessionTmp = process.env.CLAUDE_CODE_TMPDIR ?? "";

const policy: Record<string, unknown> = {
  allowPty: false,
  network: {
    // Default-deny outbound. No allowlist.
    allowedDomains: [],
    allowLocalBinding: false,
    allowLocalOutbound: false,
  },
  filesystem: {
    // Reads: STRICT deny by default (no implicit ~ exemption). Opt in to:
    //   - PWD (the agent's workspace)
    //   - /nix/store (bash, coreutils, system libs)
    //   - /tmp + session tmpdir (scratch)
    //   - /usr, /System, /private/etc (macOS system libs and dyld cache)
    //   - /Library (system frameworks)
    // No implicit access to $HOME / ~/.ssh / ~/.aws / etc.
    strictDenyRead: true,
    allowRead: [
      cwd,
      "/nix/store",
      "/tmp",
      ...(sessionTmp ? [sessionTmp] : []),
      "/usr",
      "/System",
      "/Library",
      "/private/etc",
      "/private/var/db/dyld",
      "/private/var/folders",
      "/bin",
      "/sbin",
      "/dev/null",
      "/dev/random",
      "/dev/urandom",
      "/dev/tty",
    ],
    // Writes: PWD only (plus /tmp + session tmpdir for scratch).
    allowWrite: [
      cwd,
      "/tmp",
      ...(sessionTmp ? [sessionTmp] : []),
    ],
    denyWrite: [
      "**/.env",
      "**/.env.*",
      "**/*.key",
      "**/*.pem",
      "**/*.p12",
      "**/*.pfx",
    ],
  },
  command: {
    useDefaults: true,
    // coreutils is a multicall binary on nix-darwin: blocking `chroot`
    // (in the default deny list) would collaterally block cat/head/tail/
    // etc. Skip the runtime block for chroot — preflight still catches it.
    acceptSharedBinaryCannotRuntimeDeny: ["chroot"],
    deny: [
      "git push",
      "git reset",
      "git clean",
      "git rebase",
      "git merge",
      "npm publish",
      "pnpm publish",
      "yarn publish",
      "cargo publish",
      "twine upload",
      "gem push",
      "sudo",
      "gh pr create",
      "gh pr merge",
      "gh pr close",
      "gh release create",
      "gh release delete",
      "gh repo create",
      "gh repo delete",
      "gh issue create",
      "gh issue close",
      "gh gist create",
      "gh workflow run",
      "gh api",
      "gh auth login",
      "gh secret set",
      "gh secret delete",
      "gh variable set",
      "gh variable delete",
    ],
  },
};

// ---- run -------------------------------------------------------------------

const tmpDir = sessionTmp.length > 0 ? sessionTmp : os.tmpdir();
const cfgPath = nodePath.join(
  tmpDir,
  `conch-bash-fence-${process.pid}-${Date.now()}.jsonc`,
);

let combined = "";
let exitCode = 0;
let timedOut = false;

try {
  await fsp.writeFile(cfgPath, JSON.stringify(policy, null, 2));

  // Wrap the user's command so stderr interleaves into stdout.
  const wrapped = `${command} 2>&1`;

  try {
    const result = await $`fence --settings ${cfgPath} -- bash -c ${wrapped}`
      .quiet()
      .timeout(`${timeoutSeconds}s`);
    combined = (result.stdout ?? "") + (result.stderr ?? "");
    exitCode = result.exitCode ?? 0;
  } catch (e: unknown) {
    const err = e as {
      stdout?: string;
      stderr?: string;
      exitCode?: number;
      signal?: string;
      message?: string;
    };
    combined = (err.stdout ?? "") + (err.stderr ?? "");
    exitCode = err.exitCode ?? 1;
    if (err.signal === "SIGTERM" || err.signal === "SIGKILL") {
      timedOut = true;
    }
    // No fence on PATH? zx surfaces ENOENT-on-spawn as exitCode -2 / errno
    // ENOENT in err.message — narrow check so we don't false-trigger on
    // normal child-process "command not found" stderr noise.
    const msg = err.message ?? "";
    const spawnFailed =
      msg.includes("ENOENT") &&
      (msg.includes("spawn fence") || msg.includes("spawn ENOENT"));
    if (spawnFailed) {
      emit(
        "ERROR: fence binary not found on PATH. conch.bash requires fence to enforce its sandbox policy. Aborting rather than running the command unguarded.",
        true,
      );
      process.exit(0);
    }
  }
} finally {
  await fsp.unlink(cfgPath).catch(() => {});
}

// ---- emit ------------------------------------------------------------------

if (timedOut) {
  combined += `\n--- timed out after ${timeoutSeconds}s ---\n`;
  emit(combined, true);
} else if (exitCode !== 0) {
  combined += `\n--- exit code: ${exitCode} ---\n`;
  emit(combined, true);
} else {
  emit(combined, false);
}

// ---- helpers ---------------------------------------------------------------

function emit(text: string, isError: boolean): void {
  const envelope = {
    content: [{ type: "text", text }],
    isError,
  };
  process.stdout.write(JSON.stringify(envelope) + "\n");
}
