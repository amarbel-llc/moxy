import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { $ } from "zx";

// Every failure piers reports as a one-line message rather than a stack.
export class PiersError extends Error {}

// GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow
// gws there.
export const gwsBin = process.env.GWS_BIN ?? "gws";

const oneLine = (text: string) => text.trim().replace(/\s+/g, " ");

// zx's own diagnostic for a call that never produced an exit code — most of
// it is a JS stack trace from zx's internals, not gws; only the first line
// names the actual OS failure (e.g. "E2BIG: argument list too long,
// posix_spawn '/bin/bash'", #471: a --json value gws takes as one argv
// string, over Linux's MAX_ARG_STRLEN).
const spawnFailureDetail = (message: string) => message.split("\n")[0].trim();

// Runs gws from `cwd` and parses its JSON reply. gws reports API errors on
// stdout and its own failures (auth, usage) on stderr, so a failure carries
// whichever has something to say. A call that never ran at all — the
// process failed to spawn rather than exiting non-zero, #471 — leaves both
// empty and `exitCode` null; the real reason then lives only in zx's own
// message. gws saves a non-JSON reply (a Drive delete's empty body) as a
// file in its cwd, so `cwd` must be scratch (#458).
export async function gwsJson(cwd: string, args: string[]): Promise<any> {
  const result = await $({
    cwd,
    nothrow: true,
    verbose: false,
    stdio: ["pipe", "pipe", "pipe"],
  })`${gwsBin} ${args}`;
  if (result.exitCode !== 0) {
    const detail =
      oneLine(result.stdout) ||
      oneLine(result.stderr) ||
      (result.exitCode === null
        ? spawnFailureDetail(result.message) ||
          `gws did not run (signal ${result.signal})`
        : `exited with no output (code ${result.exitCode})`);
    throw new PiersError(`gws ${args.slice(0, 3).join(" ")} failed: ${detail}`);
  }
  const stdout = result.stdout.trim();
  if (!stdout) return {};
  try {
    return JSON.parse(stdout);
  } catch {
    throw new PiersError(
      `gws ${args.slice(0, 3).join(" ")} replied with something other than JSON: ${stdout.slice(0, 200)}`,
    );
  }
}

// One Docs API batchUpdate of `documentId` (body: requests, writeControl).
export function docsBatchUpdate(
  cwd: string,
  documentId: string,
  body: Record<string, unknown>,
): Promise<any> {
  return gwsJson(cwd, [
    "docs",
    "documents",
    "batchUpdate",
    "--params",
    JSON.stringify({ documentId }),
    "--json",
    JSON.stringify(body),
  ]);
}

// Prints `value` as the tool's JSON result.
export function printResult(value: unknown): void {
  process.stdout.write(
    JSON.stringify({
      content: [
        {
          type: "text",
          text: JSON.stringify(value, null, 2),
          mimeType: "application/json",
        },
      ],
    }),
  );
}

// Runs a tool body in a scratch directory (gws's cwd), turning a PiersError
// into a one-line stderr message and exit 1.
export async function runTool(
  body: (workDir: string) => Promise<void>,
): Promise<void> {
  const workDir = await mkdtemp(join(tmpdir(), "piers-"));
  try {
    await body(workDir);
  } catch (error) {
    if (!(error instanceof PiersError)) throw error;
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  } finally {
    await rm(workDir, { recursive: true, force: true });
  }
}
