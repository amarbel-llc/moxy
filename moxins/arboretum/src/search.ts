import { $ } from "zx";
import { buildSearchInvocation } from "./astgrep.ts";

$.verbose = false;

async function main(): Promise<number> {
  const [pattern, pathArg, lang, globs, contextStr, outputMode] =
    process.argv.slice(2);

  if (!pattern) {
    throw new Error(
      "usage: search <pattern> [path] [lang] [globs] [context] [output_mode]",
    );
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
    return 0;
  }

  process.stderr.write(result.stderr);
  return result.exitCode ?? 1;
}

process.exitCode = await main();
