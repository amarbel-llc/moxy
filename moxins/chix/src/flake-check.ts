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

const result = await $`nix flake check ${extraArgs}`.nothrow();

const output = (result.stdout ?? "") + (result.stderr ?? "");
process.stdout.write(output);
if (result.exitCode !== 0) {
  process.exit(1);
}
