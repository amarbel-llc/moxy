import { $ } from "zx";

$.verbose = false;

const [flakeRef = "."] = process.argv.slice(2);

const output = (
  await $`nix flake show --json ${flakeRef} 2>/dev/null`
).stdout.trim();

const result = {
  content: [{ type: "text", text: output, mimeType: "application/json" }],
};
process.stdout.write(JSON.stringify(result));
