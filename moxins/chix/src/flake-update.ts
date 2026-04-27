import { $ } from "zx";

$.verbose = false;

const argsJson = process.argv[2] ?? "[]";
const flakeDir = process.argv[3] || ".";

let extraArgs: string[] = [];
if (argsJson && argsJson !== "null") {
  try {
    const parsed = JSON.parse(argsJson);
    if (Array.isArray(parsed)) {
      extraArgs = parsed.map((v) => String(v));
    }
  } catch {
    // malformed JSON: treat as no extra args (MCP schema should prevent this)
  }
}

$.cwd = flakeDir;

// zx expands ${extraArgs} element-wise as distinct argv entries — no shell
// parsing of element contents. .nothrow() keeps control on non-zero exit so we
// can surface the merged stderr/stdout ourselves.
const result = await $`nix flake update ${extraArgs}`.nothrow();

const output = (result.stdout ?? "") + (result.stderr ?? "");
process.stdout.write(output);
process.exitCode = result.exitCode === 0 ? 0 : 1;
