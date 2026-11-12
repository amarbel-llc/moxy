import { gwsText, printText, runGwsTool } from "../../../moxin-lib/gws-run";

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

// gws runs from a scratch directory (#458) and a failure is one line (#467):
// both come from the shared runner.
await runGwsTool(async (workDir) => {
  printText(await gwsText(workDir, args), "application/json");
});
