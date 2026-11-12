import {
  GwsError,
  gwsBin,
  gwsText,
  runGwsTool,
} from "../../../moxin-lib/gws-run";

// piers' view of the shared gws runner (moxin-lib/gws-run.ts, moxy#467): gws
// runs from a scratch directory (it saves a non-JSON reply there as
// download.<ext>, #458) and a failure is one line naming the gws command, never
// a zx stack. This file adds what only piers needs: JSON replies, the Docs
// batchUpdate call and the tool result printer.

// Every failure piers reports as a one-line message rather than a stack.
export class PiersError extends GwsError {}

// GWS_BIN is the test seam, shared with the other gws moxins.
export { gwsBin };

// Runs gws from `cwd` and parses its JSON reply. A non-JSON reply, or none at
// all (a Drive delete's empty body), is `{}` for the latter and an error for
// the former.
export async function gwsJson(cwd: string, args: string[]): Promise<any> {
  let stdout: string;
  try {
    stdout = (await gwsText(cwd, args)).trim();
  } catch (error) {
    // piers' callers catch PiersError to add context to a failed step; the
    // runner's failure carries the same one-line message.
    if (error instanceof GwsError && !(error instanceof PiersError)) {
      throw new PiersError(error.message);
    }
    throw error;
  }
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
export const runTool = runGwsTool;
