import { $ } from "zx";
import { buildSearchInvocation } from "./astgrep.ts";

$.verbose = false;

const [pattern, pathArg, lang, globs, contextStr, outputMode] = process.argv.slice(2);

if (!pattern) {
  process.stderr.write("usage: search <pattern> [path] [lang] [globs] [context] [output_mode]\n");
  process.exit(2);
}

const targetPath = pathArg && pathArg.length > 0 ? pathArg : ".";
const inv = buildSearchInvocation({
  pattern,
  lang: lang || undefined,
  globs: globs || undefined,
  context: contextStr || undefined,
  outputMode: outputMode || undefined,
});

const args = [inv.subcommand, ...inv.args, targetPath];

// ast-grep exits 1 when no matches are found. That's not a tool error —
// surface an empty result instead.
const result = await $`ast-grep ${args}`.quiet().nothrow();

if (result.exitCode === 0 || result.exitCode === 1) {
  process.stdout.write(result.stdout);
  if (result.stdout.length === 0 && result.exitCode === 1) {
    process.stdout.write("(no matches)\n");
  }
  process.exit(0);
}

process.stderr.write(result.stderr);
process.exit(result.exitCode ?? 1);
