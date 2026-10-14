import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { $ } from "zx";

// Every failure piers reports as a one-line message rather than a stack.
export class PiersError extends Error {}

// GWS_BIN is the test seam: the wrapper pins PATH, so a stub cannot shadow
// gws there.
export const gwsBin = process.env.GWS_BIN ?? "gws";

// Runs gws from `cwd` and parses its JSON reply. gws reports API errors on
// stdout, so that is what a failure carries. gws saves a non-JSON reply (a
// Drive delete's empty body) as a file in its cwd, so `cwd` must be scratch
// (#458).
export async function gwsJson(
  gws: string,
  cwd: string,
  args: string[],
): Promise<any> {
  const result = await $({
    cwd,
    nothrow: true,
    verbose: false,
    stdio: ["pipe", "pipe", "ignore"],
  })`${gws} ${args}`;
  if (result.exitCode !== 0) {
    throw new PiersError(
      `gws ${args.slice(0, 3).join(" ")} failed: ${result.stdout.trim().replace(/\s+/g, " ")}`,
    );
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
