import { $ } from "zx";

// The body of chix's `nix flake <subcommand>` tools (flake-check, flake-lock,
// flake-update). argv[2] is a JSON array of extra arguments, argv[3] the
// flake directory. nix's stdout and stderr are written merged; the exit code
// is 0 when nix succeeds and 1 otherwise.
export async function runFlakeSubcommand(subcommand: string): Promise<void> {
  $.verbose = false;
  $.cwd = process.argv[3] || ".";

  // zx expands ${extraArgs} element-wise as distinct argv entries — no shell
  // parsing of element contents. .nothrow() keeps control on non-zero exit so
  // we can surface the merged stderr/stdout ourselves.
  const extraArgs = parseExtraArgs(process.argv[2]);
  const result = await $`nix flake ${subcommand} ${extraArgs}`.nothrow();

  process.stdout.write((result.stdout ?? "") + (result.stderr ?? ""));
  process.exitCode = result.exitCode === 0 ? 0 : 1;
}

function parseExtraArgs(argsJson: string | undefined): string[] {
  if (!argsJson || argsJson === "null") {
    return [];
  }
  try {
    const parsed = JSON.parse(argsJson);
    return Array.isArray(parsed) ? parsed.map((v) => String(v)) : [];
  } catch {
    // malformed JSON: treat as no extra args (MCP schema should prevent this)
    return [];
  }
}
