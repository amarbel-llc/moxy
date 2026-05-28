import { $ } from "zx";

$.verbose = false;

const [flakeRef = "."] = process.argv.slice(2);

// .nothrow() keeps control on non-zero exit so we can surface the underlying
// nix stderr ourselves rather than letting zx throw a raw ProcessOutput.
const result = await $`nix flake show --json ${flakeRef}`.nothrow();

if (result.exitCode !== 0) {
  process.stderr.write(result.stderr);
  process.exitCode = result.exitCode ?? 1;
} else {
  const output = result.stdout.trim();
  const mcpResult = {
    content: [{ type: "text", text: output, mimeType: "application/json" }],
  };
  process.stdout.write(JSON.stringify(mcpResult));
}
