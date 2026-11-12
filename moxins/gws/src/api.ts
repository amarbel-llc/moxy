import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { join } from "path";
import { $ } from "zx";

$.verbose = false;
$.stdio = ["pipe", "pipe", "ignore"];

// GWS_BIN is the test seam, as in the piers moxin: the wrapper pins PATH, so a
// stub cannot shadow gws there.
const gwsBin = process.env.GWS_BIN ?? "gws";

const [command, params, json] = process.argv.slice(2);

const args = command.split(/\s+/);
if (params) {
  args.push("--params", params);
}
// The request body travels as one argv entry, so it may hold spaces (#515);
// splitting `command` on whitespace would break it apart.
if (json) {
  args.push("--json", json);
}

// gws saves a non-JSON 2xx reply (a Drive delete's empty body) as
// download.<ext> in its cwd, and loads a .env from there (#458), so it runs
// from a scratch directory, never from the caller's tree.
const workDir = await mkdtemp(join(tmpdir(), "gws-api-"));
try {
  const result = await $({ cwd: workDir })`${gwsBin} ${args}`;
  process.stdout.write(
    JSON.stringify({
      content: [
        { type: "text", text: result.stdout, mimeType: "application/json" },
      ],
    }),
  );
} finally {
  await rm(workDir, { recursive: true, force: true });
}
