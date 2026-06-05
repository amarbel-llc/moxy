import { $, sleep } from "zx";
import { spawn } from "node:child_process";
import { closeSync, mkdirSync, openSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { resolveRepo } from "./resolve-repo.ts";

// ci-watch: background a GitHub Actions run watch and, when the run reaches a
// terminal state, wake the agent via clown's job-wakeup channel.
//
// The tool runs in two modes, distinguished by the CI_WATCH_POLLER env var:
//
//   parent  — resolves the repo, opens a clown job, then DETACHES a poller
//             (new session, stdio redirected to a logfile) and returns the
//             {job_id, run_id, status:"watching", log} envelope immediately.
//             It must return promptly: moxy reads the tool's stdout to EOF, so
//             the detached child must NOT inherit fd0/1/2 on the moxy pipe or
//             the tool call would hang forever (cmd.Wait() never returns).
//
//   poller  — re-exec of this same entrypoint with CI_WATCH_POLLER=1. Polls
//             the run via `gh api` until it completes, maps the conclusion to a
//             clown state, and calls `clown job done`. A max-timeout guarantees
//             every job reaches a terminal `done` so none is left stuck.
//
// clown is located via ${CLOWN_BIN:-clown}: clown injects CLOWN_BIN (absolute
// path) into every plugin MCP server env, and the bare-`clown` fallback covers
// older clown releases. CLOWN_DISABLE_JOB_WAKEUP=1 is the kill switch — with no
// wake path available, the tool refuses to watch and returns status:"disabled".

$.verbose = false;

const POLLER_ENV = "CI_WATCH_POLLER";

function clownBin(): string {
  return process.env.CLOWN_BIN || "clown";
}

function logPathFor(runId: string): string {
  const stateHome =
    process.env.XDG_STATE_HOME || join(homedir(), ".local", "state");
  const dir = join(stateHome, "get-hubbed");
  mkdirSync(dir, { recursive: true });
  return join(dir, `ci-watch-${runId}.log`);
}

function writeStdout(obj: unknown): Promise<void> {
  return new Promise((resolve) => {
    process.stdout.write(JSON.stringify(obj), () => resolve());
  });
}

// Map a GitHub Actions run conclusion onto a clown job-wakeup state. Anything
// that isn't a clean success/cancel/timeout (action_required, neutral, stale,
// startup_failure, null, unknown) is surfaced as a failure so the agent is
// alerted rather than silently told the run was fine.
function mapConclusionToState(conclusion: string | null): string {
  switch (conclusion) {
    case "success":
      return "succeeded";
    case "cancelled":
      return "cancelled";
    case "timed_out":
      return "interrupted";
    case "failure":
      return "failed";
    default:
      return "failed";
  }
}

async function buildMessage(
  repo: string,
  runId: string,
  conclusion: string | null,
): Promise<string> {
  let msg = `CI ${conclusion ?? "unknown"}`;
  if (conclusion && conclusion !== "success") {
    try {
      const jobs = JSON.parse(
        (
          await $`gh api ${`repos/${repo}/actions/runs/${runId}/jobs`} --jq ${".jobs"}`
        ).stdout,
      );
      const failed = (jobs || [])
        .filter(
          (j: any) =>
            j.conclusion &&
            j.conclusion !== "success" &&
            j.conclusion !== "skipped",
        )
        .map((j: any) => j.name);
      if (failed.length) msg += `: ${failed.join(", ")}`;
    } catch (err) {
      // Best-effort enrichment — a missing /jobs response must not block the
      // terminal `done` call.
      console.error(`ci-watch: could not fetch failed jobs: ${err}`);
    }
  }
  return msg;
}

async function clownJobDone(
  jobId: string,
  state: string,
  message: string,
  runId: string,
): Promise<void> {
  const clown = clownBin();
  const resultRef = `get-hubbed ci-run-get ${runId}`;
  try {
    await $`${clown} job done ${jobId} --state ${state} --message ${message} --result-ref ${resultRef}`;
  } catch (err) {
    console.error(`ci-watch: clown job done failed: ${err}`);
  }
}

async function runPoller(): Promise<void> {
  const runId = process.argv[2];
  const repo = process.env.CI_WATCH_REPO || "";
  const jobId = process.env.CI_WATCH_JOB_ID || "";

  const pollSeconds = Number(process.env.CI_WATCH_POLL_SECONDS ?? "25");
  const timeoutSeconds = Number(
    process.env.CI_WATCH_TIMEOUT_SECONDS ?? "21600",
  );
  const sleepMs = Math.max(0, pollSeconds) * 1000;
  const deadline = Date.now() + Math.max(0, timeoutSeconds) * 1000;

  for (;;) {
    if (Date.now() >= deadline) {
      await clownJobDone(jobId, "interrupted", "watch timed out", runId);
      return;
    }

    let run: any;
    try {
      run = JSON.parse(
        (await $`gh api ${`repos/${repo}/actions/runs/${runId}`}`).stdout,
      );
    } catch (err) {
      // Transient gh/network error — log and keep polling rather than
      // abandoning the job (which would leave it stuck, never woken).
      console.error(`ci-watch: gh api error: ${err}`);
      await sleep(sleepMs);
      continue;
    }

    if (run.status === "completed") {
      const state = mapConclusionToState(run.conclusion ?? null);
      const message = await buildMessage(repo, runId, run.conclusion ?? null);
      await clownJobDone(jobId, state, message, runId);
      return;
    }

    await sleep(sleepMs);
  }
}

async function runParent(): Promise<void> {
  const [runId, repoOwnerName] = process.argv.slice(2);
  if (!runId) {
    process.stderr.write("usage: ci-watch <run_id> [<repo_owner_name>]\n");
    process.exitCode = 2;
    return;
  }

  // Kill switch: with no wake channel there's no point watching, so don't
  // background anything — just report that wakeups are disabled.
  if (process.env.CLOWN_DISABLE_JOB_WAKEUP === "1") {
    await writeStdout({ run_id: runId, status: "disabled" });
    return;
  }

  const repo = await resolveRepo(repoOwnerName);
  const clown = clownBin();

  const jobId = (
    await $`${clown} job start --source get-hubbed --label ${`ci-${runId}`}`
  ).stdout.trim();

  const log = logPathFor(runId);
  const logFd = openSync(log, "a");

  // Detach the poller: detached:true puts it in a new session (setsid), and
  // stdio ["ignore", logFd, logFd] keeps its fd0/1/2 OFF the inherited moxy
  // pipe, so moxy sees EOF on our stdout as soon as this parent exits. Re-exec
  // the same entrypoint (bun <bundle>.js <args> under the nix wrapper, or the
  // compiled binary directly) with CI_WATCH_POLLER=1 to enter poller mode.
  const child = spawn(process.execPath, process.argv.slice(1), {
    detached: true,
    stdio: ["ignore", logFd, logFd],
    env: {
      ...process.env,
      [POLLER_ENV]: "1",
      CI_WATCH_JOB_ID: jobId,
      CI_WATCH_REPO: repo,
    },
  });
  child.unref();
  closeSync(logFd);

  await writeStdout({ job_id: jobId, run_id: runId, status: "watching", log });
}

// No explicit process.exit(): the parent detaches and unref()'s the poller, so
// the event loop drains and the process exits naturally once stdout is flushed
// — keeping moxy's cmd.Wait() unblocked without tripping eslint's
// n/no-process-exit. The poller likewise exits naturally after `clown job done`.
if (process.env[POLLER_ENV] === "1") {
  await runPoller();
} else {
  await runParent();
}
