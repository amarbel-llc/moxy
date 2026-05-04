import { $ } from "zx";

$.verbose = false;

const [pattern, pathArg, lang, globs, contextStr, outputMode] = process.argv.slice(2);

if (!pattern) {
  process.stderr.write("usage: search <pattern> [path] [lang] [globs] [context] [output_mode]\n");
  process.exit(2);
}

const args: string[] = ["run", "--pattern", pattern];

if (lang) args.push("--lang", lang);
if (globs) args.push("--globs", globs);
if (contextStr) args.push("-C", contextStr);
if (outputMode === "json") args.push("--json=stream");

// ast-grep treats trailing positionals as paths; default to "." when omitted.
args.push(pathArg && pathArg.length > 0 ? pathArg : ".");

// ast-grep exits 1 when no matches are found. That's not a tool error —
// surface an empty result instead.
const result = await $`ast-grep ${args}`.nothrow();

if (result.exitCode === 0 || result.exitCode === 1) {
  process.stdout.write(result.stdout);
  if (result.stdout.length === 0 && result.exitCode === 1) {
    process.stdout.write("(no matches)\n");
  }
  process.exit(0);
}

process.stderr.write(result.stderr);
process.exit(result.exitCode ?? 1);
