import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { $ } from "zx";

// The one place the gws-calling moxins other than piers (car, prison, gmail,
// calendar, gws.api) run gws (moxy#467; piers keeps its own copy in
// moxins/piers/src/gws.ts, which #465 may extract).
//
// - gws runs from a scratch directory: it saves any non-JSON 2xx reply as
//   download.<ext> in its cwd and loads a .env from there (#458), so the
//   caller's tree must not be its cwd.
// - A failure is one line naming the gws command and its message, never a zx
//   ProcessOutput stack (#363).

// Every failure reported as a one-line message rather than a stack.
export class GwsError extends Error {}

// GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow gws.
export const gwsBin = process.env.GWS_BIN ?? "gws";

const oneLine = (text: string) => text.trim().replace(/\s+/g, " ");

// zx's own diagnostic for a call that never produced an exit code is mostly a
// JS stack trace; only its first line names the OS failure (#471).
const spawnFailureDetail = (message: string) => message.split("\n")[0].trim();

// Runs gws from `cwd` and returns its stdout. gws reports API errors on stdout
// and its own failures (auth, usage) on stderr, so a failure carries whichever
// has something to say.
export async function gwsText(cwd: string, args: string[]): Promise<string> {
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
    throw new GwsError(`gws ${args.slice(0, 3).join(" ")} failed: ${detail}`);
  }
  return result.stdout;
}

// Runs a tool body in a scratch directory (gws's cwd), turning a GwsError into
// a one-line stderr message and exit 1.
export async function runGwsTool(
  body: (workDir: string) => Promise<void>,
): Promise<void> {
  const workDir = await mkdtemp(join(tmpdir(), "gws-moxin-"));
  try {
    await body(workDir);
  } catch (error) {
    if (!(error instanceof GwsError)) throw error;
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  } finally {
    await rm(workDir, { recursive: true, force: true });
  }
}

// Prints `text` as the tool's result.
export function printText(text: string, mimeType: string): void {
  process.stdout.write(
    JSON.stringify({ content: [{ type: "text", text, mimeType }] }),
  );
}

// The common tool: one gws call whose stdout is the JSON result.
export function gwsPassthrough(args: string[]): Promise<void> {
  return runGwsTool(async (workDir) => {
    printText(await gwsText(workDir, args), "application/json");
  });
}
