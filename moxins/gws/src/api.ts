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

const result = await $`${gwsBin} ${args}`;

process.stdout.write(
  JSON.stringify({
    content: [
      { type: "text", text: result.stdout, mimeType: "application/json" },
    ],
  }),
);
