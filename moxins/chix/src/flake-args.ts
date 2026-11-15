// moxy#519: the free-form `args` of chix's `nix flake <subcommand>` tools
// bypass the flake_dir stanza the broker judges: `--flake /elsewhere`,
// `--override-input`, `--file`, `--expr`, a flake-reference positional, or any
// of the nix.conf settings nix accepts as flags (`--post-build-hook`, ...)
// reach paths and code the stanza never saw. nix accepts every setting as a
// flag, so a deny-list could never be complete; this is an allowlist of flags
// known to carry neither a path nor code. Anything else is refused before nix
// runs. Extend FLAGS deliberately, one flag and one test at a time.

// Flags that take no value.
const FLAGS = new Set([
  "--keep-going",
  "--no-build",
  "--all-systems",
  "--show-trace",
  "--print-build-logs",
  "--verbose",
  "--quiet",
  "--refresh",
  "--offline",
  "--no-update-lock-file",
  "--no-write-lock-file",
  "--no-registries",
  "--recreate-lock-file",
]);

// Flags that take one value, and what the value must look like.
const INPUT_NAME = /^[A-Za-z0-9_][A-Za-z0-9_-]*$/;
const VALUE_FLAGS = new Map<string, RegExp>([["--update-input", INPUT_NAME]]);

// Short flags: only clusters of these, none of which takes a value.
const SHORT_CLUSTER = /^-[Lvq]+$/;

// What a bare (non-flag) argument means to the subcommand: `check` and `lock`
// take a flake reference, of which only "." (the flake_dir itself) is safe;
// `update` takes input names.
export type Positional = "dot" | "inputs";

export function refuseUnsafeArgs(
  args: string[],
  positional: Positional,
): string | null {
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === "--") {
      return `refusing "--": it ends flag checking`;
    }
    if (arg.startsWith("--")) {
      if (arg.includes("=")) {
        return `refusing ${arg.split("=")[0]}=...: pass a value as its own argument, from the allowed flags`;
      }
      if (FLAGS.has(arg)) {
        continue;
      }
      const valueShape = VALUE_FLAGS.get(arg);
      if (valueShape) {
        const value = args[++i];
        if (value === undefined || !valueShape.test(value)) {
          return `refusing ${arg} ${JSON.stringify(value ?? "")}: its value must be an input name`;
        }
        continue;
      }
      return `refusing ${arg}: not an allowed flag (allowed: ${[...FLAGS, ...VALUE_FLAGS.keys()].join(", ")})`;
    }
    if (arg.startsWith("-") && arg !== "-") {
      if (SHORT_CLUSTER.test(arg)) {
        continue;
      }
      return `refusing ${arg}: not an allowed flag`;
    }
    if (positional === "dot" ? arg === "." : INPUT_NAME.test(arg)) {
      continue;
    }
    return positional === "dot"
      ? `refusing ${JSON.stringify(arg)}: a flake reference other than "." can point outside flake_dir`
      : `refusing ${JSON.stringify(arg)}: not an input name`;
  }
  return null;
}
